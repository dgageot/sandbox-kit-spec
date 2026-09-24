package assemble

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Inventory is the set of non-directory paths one kit's layers contribute,
// normalized (no leading "./" or "/"). Directories are excluded because
// overlapping directories are how composition works; overlapping files are
// how it breaks. Files includes literal whiteout markers and historical
// contributions; layer boundaries are not retained.
type Inventory struct {
	Kit   string
	Files []string
}

// zstdMagic is the zstd frame header (RFC 8878). Registries serve kit
// layers as tar+zstd when the builder (or the base image's publisher)
// chose zstd compression, so the inventory reader sniffs it beside gzip.
var zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

// LayerLink is a link entry's identity within a layer listing.
type LayerLink struct {
	Target string
	Hard   bool
}

// ReadLayerEntries lists a layer's paths by what each finally is: file,
// directory, or link. Resolution needs the distinction — a directory in
// an upper layer replaces a lower file, a symlink taking an ancestor
// redirects the paths beneath it — and the LAST entry per path decides,
// the same rule content resolution applies.
func ReadLayerEntries(r io.Reader) (files, dirs []string, links map[string]LayerLink, err error) {
	tr, closeLayer, err := openLayer(r)
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeLayer()
	const kindFile, kindDir, kindLink = 0, 1, 2
	kind := map[string]int{}
	linkOf := map[string]LayerLink{}
	var order []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read layer: %w", err)
		}
		name := strings.TrimSuffix(normalizePath(hdr.Name), "/")
		if _, seen := kind[name]; !seen {
			order = append(order, name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			kind[name] = kindDir
		case tar.TypeSymlink, tar.TypeLink:
			kind[name] = kindLink
			linkOf[name] = LayerLink{Target: hdr.Linkname, Hard: hdr.Typeflag == tar.TypeLink}
		default:
			kind[name] = kindFile
		}
	}
	links = map[string]LayerLink{}
	for _, name := range order {
		switch kind[name] {
		case kindDir:
			dirs = append(dirs, name)
		case kindLink:
			files = append(files, name)
			links[name] = linkOf[name]
		default:
			files = append(files, name)
		}
	}
	return files, dirs, links, nil
}

// ReadInventory lists the non-directory entries of one layer blob (tar,
// tar+gzip, or tar+zstd — sniffed by magic bytes). Whiteout markers retain
// their literal names so CheckCollisions can judge cross-kit deletions.
func ReadInventory(r io.Reader) ([]string, error) {
	tr, closeLayer, err := openLayer(r)
	if err != nil {
		return nil, err
	}
	defer closeLayer()

	var files []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read layer: %w", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		files = append(files, normalizePath(hdr.Name))
	}
	return files, nil
}

// ReadFile returns one path's contents from a layer blob, reporting false
// when the layer does not carry it. Callers walk a kit's layers in order
// and keep the last hit, the way the filesystem the layers compose to
// would resolve it.
func ReadFile(r io.Reader, path string) ([]byte, bool, error) {
	tr, closeLayer, err := openLayer(r)
	if err != nil {
		return nil, false, err
	}
	defer closeLayer()

	entry, err := readEntry(tr, path, true, -1)
	if err != nil {
		return nil, false, err
	}
	if entry.Linked {
		// A link's zero-length payload is not the file's contents, and
		// reporting it as such would falsely reject a conforming kit
		// staged through one. This reader sees one layer; a caller that
		// can resolve across the composed filesystem follows the target.
		return nil, false, fmt.Errorf("%s is a link to %s, which this single-layer read cannot follow", path, entry.Link)
	}
	return entry.Body, entry.OK, nil
}

// maxFileEntryBytes bounds one content read. The files read in full —
// staged descriptors — are normatively small (the same document rides a
// manifest annotation, and manifests meet registry ceilings); the bound
// exists so untrusted layers cannot make a checker allocate without
// limit. Existence-only lookups take StatFileEntry and have no bound.
const maxFileEntryBytes = 16 << 20

// ErrFileTooLarge reports a body over MaxFileEntryBytes. A caller that
// only wanted to read the file can say so rather than condemning the
// artifact for something it cannot see.
var ErrFileTooLarge = errors.New("file is larger than a checker will buffer")

// MaxFileEntryBytes is the bound, exported so other sources of the same
// filesystem enforce the one a checker expects.
const MaxFileEntryBytes = maxFileEntryBytes

// FileEntry is one path's final tar entry within a layer.
type FileEntry struct {
	Body []byte
	// Linked marks a symlink or hard-link entry independently of Link, so
	// an entry whose Linkname is empty — dangling by construction — is
	// not mistaken for an empty regular file.
	Linked bool
	// Link is the target when the entry is a symlink or hard link; Body
	// is empty then, because a link has no payload of its own.
	Link string
	// Hard distinguishes a hard link, whose target names a path from the
	// archive root, from a symlink, whose relative target resolves
	// against the entry's own directory.
	Hard bool
	// Index is the position of this (final) entry within its layer, which
	// is what bounds a hard link's target lookup.
	Index int
	// Mode is the entry's permission bits, and Uid and Gid its owner:
	// which bits apply depends on who is asking. A link's own metadata
	// says nothing; follow the target and read that one's.
	Mode     int64
	Uid, Gid int
	// Regular marks an ordinary file. A FIFO, socket, or device node
	// occupies a path and can carry execute bits, and execve runs none
	// of them.
	Regular bool
	OK      bool
}

// ReadFileEntry returns one path's final entry from a layer blob,
// preserving link-ness so a caller with a view of the whole composed
// filesystem can follow the target.
func ReadFileEntry(r io.Reader, path string) (FileEntry, error) {
	tr, closeLayer, err := openLayer(r)
	if err != nil {
		return FileEntry{}, err
	}
	defer closeLayer()
	return readEntry(tr, path, true, -1)
}

// StatFileEntryBefore is StatFileEntry over only the entries preceding
// position before: hard-link existence checks must not buffer target
// content just to learn it exists.
func StatFileEntryBefore(r io.Reader, path string, before int) (FileEntry, error) {
	tr, closeLayer, err := openLayer(r)
	if err != nil {
		return FileEntry{}, err
	}
	defer closeLayer()
	return readEntry(tr, path, false, before)
}

// ReadFileEntryBefore is ReadFileEntry over only the entries preceding
// position before. Hard-link resolution needs it: a link captures its
// target's inode at the moment the link entry applies, so a later rewrite
// of the target in the same tar must not retarget the link.
func ReadFileEntryBefore(r io.Reader, path string, before int) (FileEntry, error) {
	tr, closeLayer, err := openLayer(r)
	if err != nil {
		return FileEntry{}, err
	}
	defer closeLayer()
	return readEntry(tr, path, true, before)
}

// StatFileEntry is ReadFileEntry without content: presence and link-ness
// only, so an existence check never buffers a body and no size bound
// applies to files the caller only needs to know exist.
func StatFileEntry(r io.Reader, path string) (FileEntry, error) {
	tr, closeLayer, err := openLayer(r)
	if err != nil {
		return FileEntry{}, err
	}
	defer closeLayer()
	return readEntry(tr, path, false, -1)
}

// WalkLayer calls fn with every header of one layer blob, in archive
// order, for a caller that applies the layer the way an extractor does.
func WalkLayer(r io.Reader, fn func(*tar.Header) error) error {
	tr, closeLayer, err := openLayer(r)
	if err != nil {
		return err
	}
	defer closeLayer()
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read layer: %w", err)
		}
		if err := fn(hdr); err != nil {
			return err
		}
	}
}

// regular reports whether a tar type is an ordinary file. The reader
// rewrites the historical spelling to TypeReg before this sees it.
func regular(typeflag byte) bool {
	return typeflag == tar.TypeReg
}

// readEntry keeps the LAST matching entry, because applying a layer does:
// returning the first would report a body the composed filesystem never
// exposes.
func readEntry(tr *tar.Reader, path string, withBody bool, before int) (FileEntry, error) {
	want := normalizePath(path)
	var entry FileEntry
	for idx := 0; ; idx++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) || (before >= 0 && idx >= before) {
			return entry, nil
		}
		if err != nil {
			return FileEntry{}, fmt.Errorf("read layer: %w", err)
		}
		if strings.TrimSuffix(normalizePath(hdr.Name), "/") != strings.TrimSuffix(want, "/") {
			continue
		}
		entryIndex := idx
		if hdr.Typeflag == tar.TypeDir {
			// A directory taking the path within the same layer replaces
			// whatever file came before it.
			entry = FileEntry{}
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			entry = FileEntry{Linked: true, Link: hdr.Linkname, Hard: hdr.Typeflag == tar.TypeLink, Index: entryIndex, Mode: hdr.Mode, Uid: hdr.Uid, Gid: hdr.Gid, OK: true}
		default:
			if !withBody {
				entry = FileEntry{Index: entryIndex, Mode: hdr.Mode, Uid: hdr.Uid, Gid: hdr.Gid, Regular: regular(hdr.Typeflag), OK: true}
				continue
			}
			// Layers are untrusted input, and a tiny compressed layer can
			// advertise an enormous member: reading without a bound turns
			// a malformed artifact into an OOM instead of a finding. Only
			// content reads are bounded — the descriptor is normatively
			// small, and existence checks go through StatFileEntry.
			body, err := io.ReadAll(io.LimitReader(tr, maxFileEntryBytes+1))
			if err != nil {
				return FileEntry{}, fmt.Errorf("read %s: %w", path, err)
			}
			if len(body) > maxFileEntryBytes {
				return FileEntry{}, fmt.Errorf("%s exceeds %d bytes: %w", path, maxFileEntryBytes, ErrFileTooLarge)
			}
			entry = FileEntry{Body: body, Index: entryIndex, Mode: hdr.Mode, Uid: hdr.Uid, Gid: hdr.Gid, Regular: regular(hdr.Typeflag), OK: true}
		}
	}
}

// openLayer wraps a layer blob in a tar reader, sniffing compression by
// magic bytes: callers hand over blob bytes without their manifest media
// type.
func openLayer(r io.Reader) (*tar.Reader, func(), error) {
	br := bufio.NewReader(r)
	magic, _ := br.Peek(4)
	switch {
	case len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, nil, fmt.Errorf("open layer: %w", err)
		}
		return tar.NewReader(gz), func() { _ = gz.Close() }, nil
	case len(magic) >= 4 && bytes.Equal(magic, zstdMagic):
		zr, err := zstd.NewReader(br)
		if err != nil {
			return nil, nil, fmt.Errorf("open layer: %w", err)
		}
		return tar.NewReader(zr), zr.Close, nil
	default:
		return tar.NewReader(br), func() {}, nil
	}
}

func normalizePath(p string) string {
	p = strings.TrimPrefix(p, "./")
	return strings.TrimPrefix(p, "/")
}

// CheckCollisions checks inventories in composition order, rejecting files
// contributed by multiple kits and whiteouts deleting earlier kits' files.
// Shared directories and a kit changing its own files are allowed. Without
// layer boundaries, ownership conservatively includes every contributed
// file, even one the kit later removed from its own layers. Paths are cleaned
// for comparison without modifying the inventories.
func CheckCollisions(inventories []Inventory) error {
	owner := map[string]string{}
	collisions := map[string][]string{}
	var problems []string
	for _, inv := range inventories {
		// Check removals before recording this kit's files: a whiteout
		// must not hide a file contributed beside it in the same layer.
		for _, marker := range inv.Files {
			marker = normalizePath(path.Clean(marker))
			base := path.Base(marker)
			if !strings.HasPrefix(base, ".wh.") {
				continue
			}
			if base == ".wh." {
				return fmt.Errorf("kit %s: whiteout /%s has no target", inv.Kit, marker)
			}
			opaque := base == ".wh..wh..opq"
			target := path.Join(path.Dir(marker), strings.TrimPrefix(base, ".wh."))
			if opaque {
				target = path.Dir(marker)
			}
			for f, kit := range owner {
				if kit == inv.Kit {
					continue
				}
				// OCI root opacity hides all lower-layer children.
				if (!opaque && f == target) || strings.HasPrefix(f, target+"/") || (opaque && target == ".") {
					problems = append(problems, fmt.Sprintf("/%s: %s removes a path contributed by %s (whiteout /%s)",
						f, inv.Kit, kit, marker))
				}
			}
		}
		for _, f := range inv.Files {
			f = normalizePath(path.Clean(f))
			if strings.HasPrefix(path.Base(f), ".wh.") {
				continue
			}
			prev, taken := owner[f]
			switch {
			case !taken:
				owner[f] = inv.Kit
			case prev != inv.Kit:
				if len(collisions[f]) == 0 {
					collisions[f] = []string{prev}
				}
				if !slices.Contains(collisions[f], inv.Kit) {
					collisions[f] = append(collisions[f], inv.Kit)
				}
			}
		}
	}
	for p, kits := range collisions {
		problems = append(problems, fmt.Sprintf("/%s: contributed by %s", p, strings.Join(kits, " and ")))
	}
	if len(problems) == 0 {
		return nil
	}

	slices.Sort(problems)
	return fmt.Errorf("kit file collisions:\n  %s", strings.Join(slices.Compact(problems), "\n  "))
}
