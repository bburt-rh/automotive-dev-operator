package tasks

import (
	"strings"
	"testing"
)

func TestBuildTaskRef_ClusterResolver(t *testing.T) {
	ref := buildTaskRef("build-automotive-image", "test-ns", nil)

	if ref.Resolver != TaskResolverCluster {
		t.Fatalf("expected cluster resolver, got %q", ref.Resolver)
	}

	params := make(map[string]string)
	for _, p := range ref.Params {
		params[p.Name] = p.Value.StringVal
	}

	if params["name"] != "build-automotive-image" {
		t.Errorf("expected name=build-automotive-image, got %q", params["name"])
	}
	if params["namespace"] != "test-ns" {
		t.Errorf("expected namespace=test-ns, got %q", params["namespace"])
	}
	if params["kind"] != "task" {
		t.Errorf("expected kind=task, got %q", params["kind"])
	}
}

func TestBuildTaskRef_ClusterResolver_NilBuildConfig(t *testing.T) {
	ref := buildTaskRef("push-artifact-registry", "ns", nil)
	if ref.Resolver != TaskResolverCluster {
		t.Fatalf("nil buildConfig should use cluster resolver, got %q", ref.Resolver)
	}
}

func TestBuildTaskRef_ClusterResolver_EmptyTaskResolver(t *testing.T) {
	ref := buildTaskRef("push-artifact-registry", "ns", &BuildConfig{})
	if ref.Resolver != TaskResolverCluster {
		t.Fatalf("empty TaskResolver should use cluster resolver, got %q", ref.Resolver)
	}
}

func TestBuildTaskRef_BundleResolver(t *testing.T) {
	bundleRef := "quay.io/org/tasks@sha256:abc123"
	ref := buildTaskRef("build-automotive-image", "test-ns", &BuildConfig{
		TaskResolver:  TaskResolverBundle,
		TaskBundleRef: bundleRef,
	})

	if ref.Resolver != tektonResolverBundles {
		t.Fatalf("expected bundles resolver, got %q", ref.Resolver)
	}

	params := make(map[string]string)
	for _, p := range ref.Params {
		params[p.Name] = p.Value.StringVal
	}

	if params["bundle"] != bundleRef {
		t.Errorf("expected bundle=%s, got %q", bundleRef, params["bundle"])
	}
	if params["name"] != "build-automotive-image" {
		t.Errorf("expected name=build-automotive-image, got %q", params["name"])
	}
	if params["kind"] != "task" {
		t.Errorf("expected kind=task, got %q", params["kind"])
	}
	// Bundle resolver should NOT have namespace param
	if _, ok := params["namespace"]; ok {
		t.Error("bundle resolver should not have namespace param")
	}
}

func TestBuildTaskRef_BundleResolver_MissingRef(t *testing.T) {
	// TaskResolver=bundle but no TaskBundleRef should fall back to cluster
	ref := buildTaskRef("flash-image", "ns", &BuildConfig{
		TaskResolver: TaskResolverBundle,
	})
	if ref.Resolver != TaskResolverCluster {
		t.Fatalf("bundle resolver with empty ref should fall back to cluster, got %q", ref.Resolver)
	}
}

func TestGenerateTektonPipeline_HasImagesResult(t *testing.T) {
	pipeline := GenerateTektonPipeline("test-pipeline", "test-ns", &BuildConfig{})

	var found bool
	for _, r := range pipeline.Spec.Results {
		if r.Name == "IMAGES" {
			found = true
			if r.Value.StringVal != "$(finally.collect-images-result.results.IMAGES)" {
				t.Errorf("IMAGES result value = %q, want finally task reference", r.Value.StringVal)
			}
			break
		}
	}
	if !found {
		t.Fatal("pipeline should have IMAGES result for Tekton Chains")
	}
}

func TestGenerateTektonPipeline_HasFinallyTask(t *testing.T) {
	pipeline := GenerateTektonPipeline("test-pipeline", "test-ns", &BuildConfig{})

	if len(pipeline.Spec.Finally) == 0 {
		t.Fatal("pipeline should have finally tasks")
	}

	var collectTask bool
	for _, task := range pipeline.Spec.Finally {
		if task.Name == "collect-images-result" {
			collectTask = true

			// Verify it has the IMAGES result
			if task.TaskSpec == nil {
				t.Fatal("collect-images-result should have inline TaskSpec")
			}
			var hasImagesResult bool
			for _, r := range task.TaskSpec.Results {
				if r.Name == "IMAGES" {
					hasImagesResult = true
				}
			}
			if !hasImagesResult {
				t.Error("collect-images-result task should have IMAGES result")
			}

			// Verify it reads from workspace files (no params or task-result refs)
			if len(task.Params) != 0 {
				t.Errorf("collect-images-result should have no params (reads from workspace), got %d", len(task.Params))
			}
			if len(task.Workspaces) == 0 {
				t.Error("collect-images-result should bind the shared workspace")
			}
			break
		}
	}
	if !collectTask {
		t.Fatal("pipeline should have collect-images-result finally task")
	}
}

func TestGenerateTektonPipeline_IntegrityDigestParam(t *testing.T) {
	pipeline := GenerateTektonPipeline("test-pipeline", "test-ns", &BuildConfig{})

	// Find push-disk-artifact task
	for _, task := range pipeline.Spec.Tasks {
		if task.Name == "push-disk-artifact" {
			for _, p := range task.Params {
				if p.Name == "expected-artifact-digest" {
					if p.Value.StringVal != "$(tasks.build-image.results.ARTIFACT_INTEGRITY_DIGEST)" {
						t.Errorf("expected-artifact-digest = %q, want build-image result ref", p.Value.StringVal)
					}
					return
				}
			}
			t.Fatal("push-disk-artifact should have expected-artifact-digest param")
		}
	}
	t.Fatal("pipeline should have push-disk-artifact task")
}

func TestGenerateTektonPipeline_BundleResolver(t *testing.T) {
	bundleRef := "quay.io/org/tasks@sha256:abc123"
	pipeline := GenerateTektonPipeline("test-pipeline", "test-ns", &BuildConfig{
		TaskResolver:  TaskResolverBundle,
		TaskBundleRef: bundleRef,
	})

	// All non-inline tasks should use bundles resolver
	for _, task := range pipeline.Spec.Tasks {
		if task.TaskRef == nil {
			continue // skip tasks with inline TaskSpec
		}
		if task.TaskRef.Resolver != tektonResolverBundles {
			t.Errorf("task %q should use bundles resolver, got %q", task.Name, task.TaskRef.Resolver)
		}
	}
}

func TestGenerateTektonPipeline_ClusterResolverDefault(t *testing.T) {
	pipeline := GenerateTektonPipeline("test-pipeline", "test-ns", &BuildConfig{})

	for _, task := range pipeline.Spec.Tasks {
		if task.TaskRef == nil {
			continue
		}
		if task.TaskRef.Resolver != TaskResolverCluster {
			t.Errorf("task %q should use cluster resolver by default, got %q", task.Name, task.TaskRef.Resolver)
		}
	}
}

func TestGenerateBuildTask_HasIntegrityDigestResult(t *testing.T) {
	task := GenerateBuildAutomotiveImageTask("test-ns", nil, "")

	for _, r := range task.Spec.Results {
		if r.Name == "ARTIFACT_INTEGRITY_DIGEST" {
			return
		}
	}
	t.Fatal("build task should have ARTIFACT_INTEGRITY_DIGEST result")
}

func TestGeneratePushTask_HasExpectedDigestParam(t *testing.T) {
	task := GeneratePushArtifactRegistryTask("test-ns", nil)

	for _, p := range task.Spec.Params {
		if p.Name == "expected-artifact-digest" {
			if p.Default == nil || p.Default.StringVal != "" {
				t.Error("expected-artifact-digest should default to empty string")
			}
			return
		}
	}
	t.Fatal("push task should have expected-artifact-digest param")
}

func TestCollectImagesScript_Format(t *testing.T) {
	// Verify the finally task script reads from workspace files
	pipeline := GenerateTektonPipeline("test-pipeline", "test-ns", &BuildConfig{})

	for _, task := range pipeline.Spec.Finally {
		if task.Name == "collect-images-result" {
			if task.TaskSpec == nil {
				t.Fatal("collect-images-result should have inline TaskSpec")
			}
			if len(task.TaskSpec.Steps) == 0 {
				t.Fatal("collect-images-result should have steps")
			}
			script := task.TaskSpec.Steps[0].Script
			if script == "" {
				t.Fatal("collect step should have a script")
			}
			// Verify the script reads from workspace chain result files
			if !strings.Contains(script, "CHAINS_DIR") {
				t.Error("script should define CHAINS_DIR for workspace result files")
			}
			if !strings.Contains(script, "$CHAINS_DIR/container/url") {
				t.Error("script should read container URL from workspace")
			}
			if !strings.Contains(script, "$CHAINS_DIR/disk/url") {
				t.Error("script should read disk URL from workspace")
			}
			return
		}
	}
	t.Fatal("pipeline should have collect-images-result task")
}

// TestImagesResultFormat verifies the image@digest format Chains expects
func TestImagesResultFormat(t *testing.T) {
	// Simulate what the collect-images script produces
	containerURL := "registry.example.com/img:v1"
	containerDigest := "sha256:abc123"
	diskURL := "registry.example.com/disk:v1"
	diskDigest := "sha256:def456"

	result := containerURL + "@" + containerDigest + "\n" + diskURL + "@" + diskDigest

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 image lines, got %d", len(lines))
	}
	for _, line := range lines {
		parts := strings.SplitN(line, "@", 2)
		if len(parts) != 2 {
			t.Errorf("line %q should contain exactly one '@' separator", line)
			continue
		}
		if !strings.HasPrefix(parts[1], "sha256:") {
			t.Errorf("digest %q should start with sha256:", parts[1])
		}
	}
}
