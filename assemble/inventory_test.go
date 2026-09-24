package assemble

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckCollisionsRejectsWhiteouts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		marker string
		files  []string
	}{
		{
			name:   "file deletion",
			marker: "usr/local/bin/.wh.tool",
			files:  []string{"usr/local/bin/tool"},
		},
		{
			name:   "directory deletion",
			marker: "opt/.wh.tools",
			files:  []string{"opt/tools/bin/tool", "opt/tools/config"},
		},
		{
			name:   "opaque directory",
			marker: "opt/tools/.wh..wh..opq",
			files:  []string{"opt/tools/bin/tool", "opt/tools/config"},
		},
		{
			name:   "opaque root",
			marker: ".wh..wh..opq",
			files:  []string{"tool", "usr/local/bin/tool"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := map[string]string{}
			for _, file := range tc.files {
				entries[file] = "workload content"
			}
			base, err := ReadInventory(bytes.NewReader(tarLayer(t, false, entries)))
			require.NoError(t, err)
			mixin, err := ReadInventory(bytes.NewReader(tarLayer(t, false, map[string]string{tc.marker: ""})))
			require.NoError(t, err)

			err = CheckCollisions([]Inventory{
				{Kit: "workload", Files: base},
				{Kit: "mixin", Files: mixin},
			})
			require.Error(t, err)
			for _, file := range tc.files {
				require.ErrorContains(t, err, "/"+file)
			}
			require.ErrorContains(t, err, "mixin removes a path contributed by workload")
			require.ErrorContains(t, err, "whiteout /"+tc.marker)
		})
	}
}

func TestCheckCollisionsCanonicalizesWhiteoutPaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		file   string
		marker string
	}{
		{"dot in owned path", "opt/./tools/file", "opt/.wh.tools"},
		{"parent in owned path", "opt/unused/../tools/file", "opt/.wh.tools"},
		{"repeated separator in owned path", "opt//tools/file", "opt/tools/.wh.file"},
		{"repeated leading dot in owned path", "././opt/tools/file", "opt/.wh.tools"},
		{"absolute owned path", "/./opt/tools/file", "opt/.wh.tools"},
		{"dot in marker", "opt/tools/file", "opt/./tools/.wh.file"},
		{"parent in marker", "opt/tools/file", "opt/unused/../.wh.tools"},
		{"repeated separator in marker", "opt/tools/file", "opt//tools/.wh.file"},
		{"trailing dot in marker", "opt/tools/file", "opt/tools/.wh.file/."},
		{"absolute marker", "opt/tools/file", "/./opt/.wh.tools"},
		{"opaque directory", "opt/./tools/file", "opt//tools/.wh..wh..opq/."},
		{"opaque root", "opt/tools/file", "././.wh..wh..opq/."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := ReadInventory(bytes.NewReader(tarLayer(t, false, map[string]string{tc.file: "content"})))
			require.NoError(t, err)
			mixin, err := ReadInventory(bytes.NewReader(tarLayer(t, false, map[string]string{tc.marker: ""})))
			require.NoError(t, err)
			baseBefore, mixinBefore := append([]string(nil), base...), append([]string(nil), mixin...)

			err = CheckCollisions([]Inventory{
				{Kit: "workload", Files: base},
				{Kit: "mixin", Files: mixin},
			})
			require.ErrorContains(t, err, "/opt/tools/file: mixin removes a path contributed by workload")
			require.Equal(t, baseBefore, base, "checking must not rewrite cached inventories")
			require.Equal(t, mixinBefore, mixin, "checking must not rewrite cached inventories")
		})
	}
}

func TestCheckCollisionsCanonicalizesSharedFiles(t *testing.T) {
	err := CheckCollisions([]Inventory{
		{Kit: "workload", Files: []string{"opt/./tools/file"}},
		{Kit: "mixin", Files: []string{"opt//tools/file"}},
	})
	require.EqualError(t, err, "kit file collisions:\n  /opt/tools/file: contributed by workload and mixin")
}

func TestCheckCollisionsRejectsDeletionOfAnEarlierMixin(t *testing.T) {
	err := CheckCollisions([]Inventory{
		{Kit: "workload", Files: []string{"bin/sh"}},
		{Kit: "tool-kit", Files: []string{"opt/tool/bin"}},
		{Kit: "cleanup-kit", Files: []string{"opt/.wh.tool"}},
	})
	require.ErrorContains(t, err, "/opt/tool/bin")
	require.ErrorContains(t, err, "cleanup-kit removes a path contributed by tool-kit")
}

func TestCheckCollisionsAllowsWhiteouts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		inventories []Inventory
	}{
		{
			name: "deletes its own file",
			inventories: []Inventory{
				{Kit: "workload", Files: []string{"bin/sh"}},
				{Kit: "mixin", Files: []string{"opt/tool", "opt/.wh.tool"}},
			},
		},
		{
			name: "creates after its own deletion",
			inventories: []Inventory{
				{Kit: "mixin", Files: []string{"opt/.wh.tool", "opt/tool"}},
			},
		},
		{
			name: "opaque directory with its own files",
			inventories: []Inventory{
				{Kit: "mixin", Files: []string{"opt/tool", "opt/.wh..wh..opq", "opt/other"}},
			},
		},
		{
			name: "opaque root with its own files",
			inventories: []Inventory{
				{Kit: "workload", Files: []string{"bin/sh", ".wh..wh..opq", "etc/config"}},
			},
		},
		{
			name: "later kit creates a previously deleted path",
			inventories: []Inventory{
				{Kit: "workload", Files: []string{"opt/.wh.tool", "other/.wh..wh..opq"}},
				{Kit: "mixin", Files: []string{"opt/tool/bin", "other/config"}},
			},
		},
		{
			name: "repeated deletions do not claim files",
			inventories: []Inventory{
				{Kit: "first", Files: []string{"opt/.wh.tool", "other/.wh..wh..opq"}},
				{Kit: "second", Files: []string{"opt/.wh.tool", "other/.wh..wh..opq"}},
			},
		},
		{
			name: "canonicalized deletion does not claim a file",
			inventories: []Inventory{
				{Kit: "first", Files: []string{"opt/.wh.tool/."}},
				{Kit: "second", Files: []string{"opt/.wh.tool/.", "opt/tool"}},
			},
		},
		{
			name: "canonicalized deletion of its own file",
			inventories: []Inventory{
				{Kit: "workload", Files: []string{"bin/sh"}},
				{Kit: "mixin", Files: []string{"opt/./tool", "opt/.wh.tool/."}},
			},
		},
		{
			name: "similarly named siblings are not deleted",
			inventories: []Inventory{
				{Kit: "workload", Files: []string{"opt/toolshed/bin"}},
				{Kit: "mixin", Files: []string{"opt/.wh.tools", "opt/tools/.wh..wh..opq"}},
			},
		},
		{
			name: "whiteout prefix belongs to the basename",
			inventories: []Inventory{
				{Kit: "workload", Files: []string{"opt/tools/file", "opt/tool"}},
				{Kit: "mixin", Files: []string{"opt/.wh.tools/file", "opt/not.wh.tool"}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, CheckCollisions(tc.inventories))
		})
	}
}

func TestCheckCollisionsWhiteoutChecksHistoricalContributions(t *testing.T) {
	err := CheckCollisions([]Inventory{
		{Kit: "workload", Files: []string{"opt/tool", "opt/.wh.tool"}},
		{Kit: "mixin", Files: []string{"opt/.wh.tool"}},
	})
	require.ErrorContains(t, err, "/opt/tool")
	require.ErrorContains(t, err, "mixin removes a path contributed by workload")
}

func TestCheckCollisionsRejectsEmptyWhiteoutTarget(t *testing.T) {
	err := CheckCollisions([]Inventory{{Kit: "mixin", Files: []string{"opt/.wh."}}})
	require.EqualError(t, err, "kit mixin: whiteout /opt/.wh. has no target")
}

func TestCheckCollisionsRejectsEmptyRootWhiteoutTarget(t *testing.T) {
	err := CheckCollisions([]Inventory{{Kit: "mixin", Files: []string{".wh."}}})
	require.EqualError(t, err, "kit mixin: whiteout /.wh. has no target")
}

func TestCheckCollisionsWhiteoutDiagnostics(t *testing.T) {
	err := CheckCollisions([]Inventory{
		{Kit: "workload", Files: []string{"opt/b", "opt/a", "opt/c"}},
		{Kit: "mixin", Files: []string{"opt/.wh.b", "opt/.wh.a", "opt/.wh.a", "opt/c"}},
	})
	require.EqualError(t, err, "kit file collisions:\n"+
		"  /opt/a: mixin removes a path contributed by workload (whiteout /opt/.wh.a)\n"+
		"  /opt/b: mixin removes a path contributed by workload (whiteout /opt/.wh.b)\n"+
		"  /opt/c: contributed by workload and mixin")
}
