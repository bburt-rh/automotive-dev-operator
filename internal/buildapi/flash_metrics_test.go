package buildapi

import (
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // Dot import is standard for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive // Dot import is standard for Gomega
	io_prometheus_client "github.com/prometheus/client_model/go"
)

var _ = Describe("Flash Metrics", func() {
	var server *APIServer

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		server = NewAPIServer(":0", logr.Discard())
	})

	Context("metrics endpoint", func() {
		It("should expose prometheus metrics at /metrics", func() {
			// Ensure at least one observation so the histogram appears
			FlashRequestDuration.WithLabelValues("/v1/flash", "200").Observe(0.001)

			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			body := w.Body.String()
			Expect(body).To(ContainSubstring("ado_flash_created_total"))
			Expect(body).To(ContainSubstring("ado_flash_request_duration_seconds"))
		})
	})

	Context("FlashCreatedTotal", func() {
		It("should increment on flash creation", func() {
			m := &io_prometheus_client.Metric{}
			Expect(FlashCreatedTotal.Write(m)).To(Succeed())
			before := m.GetCounter().GetValue()

			FlashCreatedTotal.Inc()

			m = &io_prometheus_client.Metric{}
			Expect(FlashCreatedTotal.Write(m)).To(Succeed())
			after := m.GetCounter().GetValue()
			Expect(after - before).To(Equal(float64(1)))
		})
	})

	Context("FlashRequestDuration", func() {
		It("should record request duration with endpoint and status labels", func() {
			FlashRequestDuration.WithLabelValues("/v1/flash", "200").Observe(0.5)

			m := &io_prometheus_client.Metric{}
			obs, err := FlashRequestDuration.GetMetricWithLabelValues("/v1/flash", "200")
			Expect(err).NotTo(HaveOccurred())
			Expect(obs.(interface {
				Write(*io_prometheus_client.Metric) error
			}).Write(m)).To(Succeed())
			Expect(m.GetHistogram().GetSampleCount()).To(BeNumerically(">=", 1))
		})
	})
})
