package buildapi

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	automotivev1alpha1 "github.com/centos-automotive-suite/automotive-dev-operator/api/v1alpha1"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
)

func writeJSON(c *gin.Context, status int, v any) {
	c.Header("Cache-Control", "no-store")
	c.IndentedJSON(status, v)
}

const maxPageLimit = 500

// parsePagination extracts limit and offset from query parameters.
// When limit is not provided, 0 is returned and applyPagination returns
// the full slice (preserving backward compatibility for existing clients).
// When provided, limit is clamped to maxPageLimit (500).
func parsePagination(c *gin.Context) (limit, offset int) {
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
			if limit > maxPageLimit {
				limit = maxPageLimit
			}
		}
	}
	if o := c.Query("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}

// applyPagination returns the paginated window of items. A limit of 0
// means "no limit" — the full slice (from offset) is returned.
func applyPagination[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	if limit <= 0 {
		return items[offset:]
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func parseSinceTime(sinceParam string) *metav1.Time {
	if sinceParam == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, sinceParam)
	if err != nil {
		return nil
	}
	return &metav1.Time{Time: t}
}

func resolveNamespace() string {
	if ns := strings.TrimSpace(os.Getenv("BUILD_API_NAMESPACE")); ns != "" {
		return ns
	}
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		ns := strings.TrimSpace(string(data))
		if ns != "" {
			return ns
		}
	}
	return "default"
}

func getRESTConfigFromRequest(_ *gin.Context) (*rest.Config, error) {
	var cfg *rest.Config
	var err error
	cfg, err = rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build kube config: %w", err)
		}
	}
	cfgCopy := rest.CopyConfig(cfg)
	cfgCopy.Timeout = 30 * time.Minute
	return cfgCopy, nil
}

// getKubernetesClient creates a controller-runtime client for accessing Kubernetes resources
func getKubernetesClient() (client.Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build kube config: %w", err)
		}
	}

	scheme := runtime.NewScheme()
	if err := automotivev1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add scheme: %w", err)
	}

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}
	return k8sClient, nil
}

func getClientFromRequest(c *gin.Context) (client.Client, error) {
	cfg, err := getRESTConfigFromRequest(c)
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	if err := automotivev1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add automotive scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add core scheme: %w", err)
	}
	if err := tektonv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add tekton scheme: %w", err)
	}

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}
	return k8sClient, nil
}

// setupLogStreamHeaders configures HTTP headers for log streaming
func setupLogStreamHeaders(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	c.Writer.Header().Set("Pragma", "no-cache")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write([]byte("Waiting for logs...\n"))
	c.Writer.Flush()
}

// Shell metacharacters that must be blocked to prevent injection attacks
var shellMetachars = []string{";", "|", "&", "$", "`", "(", ")", "{", "}", "<", ">", "!", "\\", "'", "\"", "\n", "\r"}

// validateInput validates a string for dangerous characters and length
func validateInput(value, fieldName string, maxLen int, allowEmpty bool, extraChars ...string) error {
	if value == "" {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("%s is required", fieldName)
	}

	// Combine shell metacharacters with any additional blocked characters
	blockedChars := append(shellMetachars, extraChars...)
	for _, char := range blockedChars {
		if strings.Contains(value, char) {
			return fmt.Errorf("%s contains invalid character: %q", fieldName, char)
		}
	}

	if len(value) > maxLen {
		return fmt.Errorf("%s too long (max %d characters)", fieldName, maxLen)
	}
	return nil
}

func validateContainerRef(ref string) error {
	return validateInput(ref, "container reference", 500, true)
}

func validateBuildName(name string) error {
	if err := validateInput(name, "build name", 253, false, "/"); err != nil {
		return err
	}

	sanitized := sanitizeBuildNameForValidation(name)
	if sanitized == "" {
		return fmt.Errorf("build name contains only invalid characters")
	}

	return nil
}

func sanitizeBuildNameForValidation(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := strings.ReplaceAll(b.String(), "--", "-")
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return strings.Trim(result, "-")
}

func (a *APIServer) resolveRequester(c *gin.Context) string {
	if v, ok := c.Get("requester"); ok {
		if username, ok := v.(string); ok && username != "" {
			return username
		}
	}
	return "unknown"
}
