package spec

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectionTargetsIdentifyConsumedMembers(t *testing.T) {
	for _, optional := range []bool{false, true} {
		for _, reject := range []bool{false, true} {
			t.Run(fmt.Sprintf("optional=%t/reject=%t", optional, reject), func(t *testing.T) {
				ordinary := Capability{Type: "com.example/ordinary@1"}
				first := Capability{Type: "com.example/first@1"}
				second := Capability{Type: "com.example/second@1"}
				// Re-exported source paths can differ from the consumed layout,
				// and identical labels cannot identify distinct declarations.
				for _, c := range []*Capability{&ordinary, &first, &second} {
					c.Source = &CapabilitySource{Kit: "original-publisher", Path: "capabilities[99]"}
				}
				d := &Descriptor{Kind: KindWorkload, Capabilities: []Capability{
					ordinary,
					{Group: &CapabilityGroup{Optional: optional, Capabilities: []Capability{first, second}}},
					{Type: "com.example/last@1"},
				}}
				want := []SelectionTarget{
					{Reference: "consumed-kit:tag", Path: "capabilities[0]"},
					{Reference: "consumed-kit:tag", Path: "capabilities[1].group.capabilities[0]"},
					{Reference: "consumed-kit:tag", Path: "capabilities[1].group.capabilities[1]"},
					{Reference: "consumed-kit:tag", Path: "capabilities[2]"},
				}
				var got []SelectionTarget
				selection, err := SelectCapabilities(t.Context(), "consumed-kit:tag", d,
					func(_ context.Context, target SelectionTarget, _ Descriptor, _ Capability) CapabilityDecision {
						got = append(got, target)
						return CapabilityDecision{Accepted: !reject || target.Path != want[2].Path}
					})
				if reject && !optional {
					require.ErrorContains(t, err, "required capability selection rejected")
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, want, got)
				var paths []string
				for _, records := range [][]SelectionRecord{selection.Selected, selection.Skipped} {
					for _, record := range records {
						paths = append(paths, record.Members...)
					}
				}
				require.ElementsMatch(t, []string{want[0].Path, want[1].Path, want[2].Path, want[3].Path}, paths)
				if reject {
					require.Len(t, selection.Skipped, 1)
					require.Equal(t, []CapabilityDecision{{Accepted: true}, {}}, selection.Skipped[0].Decisions)
				}
			})
		}
	}
}
