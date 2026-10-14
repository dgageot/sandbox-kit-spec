package fetch

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/sandbox-kit-spec/v3/spec"
	"github.com/stretchr/testify/require"
)

func TestSelectionTargetsAndExpandedDescriptorsThroughPublicAPIs(t *testing.T) {
	for _, method := range []string{"Resolve", "ResolvePartial", "Assemble"} {
		t.Run(method, func(t *testing.T) {
			reg := newRegistry(t)
			base, _ := assemblyFixture(t, spec.KindWorkload, "base", "base")
			mixin, _ := assemblyFixture(t, spec.KindMixin, "feature", "feature")
			port := "9000"
			cache := "/cache"
			d := &spec.Descriptor{SchemaVersion: spec.SchemaVersion, Kind: spec.KindMixin,
				Args: map[string]spec.Arg{"port": {Default: &port}, "cache": {Default: &cache, Env: "CACHE_PATH"}},
				Capabilities: []spec.Capability{
					{Type: spec.CapabilityPort, Source: &spec.CapabilitySource{Kit: "original", Path: "capabilities[7]"},
						Config: map[string]any{"container": "${{ kit.args.port }}"}},
					{Source: &spec.CapabilitySource{Path: "capabilities[8]"}, Group: &spec.CapabilityGroup{Optional: true, Capabilities: []spec.Capability{
						{Type: spec.CapabilityLifecycle, Source: &spec.CapabilitySource{Kit: "original", Path: "capabilities[9]"},
							Config: map[string]any{"files": []any{map[string]any{"path": "${{ kit.env.CACHE_PATH }}/settings", "content": "yes"}}}},
						{Type: spec.CapabilityAgentContext, Config: map[string]any{"content": "skipped guidance"}},
					}}},
				}}
			mixin.Manifest.Annotations[spec.AnnotationDescriptor] = string(kitJSON(t, d))
			for repo, input := range map[string]*LoadedKit{"base": base, "feature": mixin} {
				manifest, err := json.Marshal(input.Manifest)
				require.NoError(t, err)
				config, err := json.Marshal(input.Config)
				require.NoError(t, err)
				reg.blobs[repo+"/blobs/"+input.Manifest.Config.Digest.String()] = config
				reg.tag(repo, "first", manifest)
				reg.tag(repo, "second", manifest)
			}
			client, err := New()
			require.NoError(t, err)
			first, second := reg.ref("feature", "first"), reg.ref("feature", "second")
			requests := reqs(reg.ref("base", "first"), first, second)
			// The two tags resolve to the same artifact and publisher labels;
			// selection can still address their declarations independently.
			targets := map[spec.SelectionTarget]bool{}
			selector := func(_ context.Context, target spec.SelectionTarget, _ spec.Descriptor, c spec.Capability) spec.CapabilityDecision {
				require.Contains(t, []string{first, second}, target.Reference)
				require.False(t, targets[target], "a member is selected once")
				targets[target] = true
				if c.Type == spec.CapabilityPort {
					var config spec.Port
					require.NoError(t, spec.DecodeCapabilityConfig(c, &config))
					require.Equal(t, 9000, config.Container)
				}
				if c.Type == spec.CapabilityLifecycle {
					var config spec.Lifecycle
					require.NoError(t, spec.DecodeCapabilityConfig(c, &config))
					require.Equal(t, "/cache/settings", config.Files[0].Path)
				}
				return spec.CapabilityDecision{Accepted: target.Path != "capabilities[1].group.capabilities[0]"}
			}
			var result *Resolved
			switch method {
			case "Resolve":
				result, err = client.Resolve(t.Context(), requests, WithCapabilitySelector(selector))
			case "ResolvePartial":
				result, err = client.ResolvePartial(t.Context(), requests[1:], WithCapabilitySelector(selector))
			case "Assemble":
				var assembled *Result
				assembled, err = Assemble(t.Context(), requests, Options{Loader: client.LoadKit, CapabilitySelector: selector})
				if err == nil {
					result = assembled.Resolved
				}
			}
			require.NoError(t, err)
			require.Len(t, targets, 6)
			for i, selection := range result.Selections {
				expanded := selection.ExpandedDescriptor
				require.NotNil(t, expanded)
				if selection.Reference == requests[0].Reference {
					require.Empty(t, expanded.Capabilities, "kits without declarations retain an expanded descriptor")
					continue
				}
				for _, record := range append(selection.Selection.Selected, selection.Selection.Skipped...) {
					for _, path := range record.Members {
						require.True(t, targets[spec.SelectionTarget{Reference: selection.Reference, Path: path}])
					}
				}
				require.Len(t, expanded.Capabilities, 2)
				require.Len(t, expanded.Capabilities[1].Group.Capabilities, 2)
				require.Len(t, result.Kits[i].Descriptor.Capabilities, 1)
				var expandedPort spec.Port
				require.NoError(t, spec.DecodeCapabilityConfig(expanded.Capabilities[0], &expandedPort))
				require.Equal(t, 9000, expandedPort.Container)
				var config spec.Lifecycle
				require.NoError(t, spec.DecodeCapabilityConfig(expanded.Capabilities[1].Group.Capabilities[0], &config))
				require.Equal(t, "/cache/settings", config.Files[0].Path)
				require.Equal(t, d.Capabilities[0].Source, expanded.Capabilities[0].Source)
				require.Equal(t, d.Capabilities[1].Source, expanded.Capabilities[1].Source, "path-only sources stay path-only")
				require.Equal(t, d.Capabilities[1].Group.Capabilities[0].Source, expanded.Capabilities[1].Group.Capabilities[0].Source)
				require.Nil(t, expanded.Capabilities[1].Group.Capabilities[1].Source, "missing sources are not filled in")
				require.JSONEq(t, string(kitJSON(t, d)), string(kitJSON(t, selection.PublishedDescriptor)))
				require.Len(t, selection.Selection.Skipped, 1)
				require.Equal(t, []spec.CapabilityDecision{{}, {Accepted: true}}, selection.Selection.Skipped[0].Decisions)
			}
		})
	}
}
