package monitoring

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const routeTagKey = "route_tag"

var relayErrorReasonWhitelist = map[string]struct{}{
	"none":              {},
	"token_missing":     {},
	"token_invalid":     {},
	"token_expired":     {},
	"token_exhausted":   {},
	"token_disabled":    {},
	"token_auth_failed": {},
}

var (
	registerOnce sync.Once
	probeOnce    sync.Once

	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "newapi_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"tag", "method", "route", "status_code", "status_class", "error_reason"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "newapi_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tag", "method", "route"},
	)

	httpInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "newapi_http_inflight_requests",
			Help: "In-flight HTTP requests",
		},
		[]string{"tag"},
	)

	relayRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "newapi_relay_requests_total",
			Help: "Total number of relay requests grouped by downstream channel type",
		},
		[]string{"channel_type", "channel_id", "channel_name", "status_class"},
	)

	relayRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "newapi_relay_request_duration_seconds",
			Help:    "Relay request duration in seconds grouped by downstream channel type",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"channel_type", "channel_id", "channel_name"},
	)

	relayErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "newapi_relay_errors_total",
			Help: "Total number of relay requests with 5xx response",
		},
		[]string{"channel_type", "channel_id", "channel_name"},
	)

	dependencyEnabled = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "newapi_dependency_enabled",
			Help: "Whether dependency is enabled in runtime config (1 enabled, 0 disabled)",
		},
		[]string{"dependency"},
	)

	dependencyUp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "newapi_dependency_up",
			Help: "Whether dependency health probe is successful (1 up, 0 down)",
		},
		[]string{"dependency"},
	)

	dbConnections = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "newapi_db_connections",
			Help: "Database connection pool stats",
		},
		[]string{"state"},
	)

	dbWaitTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "newapi_db_wait_count",
			Help: "Database wait count from sql.DB stats",
		},
	)

	dbWaitDuration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "newapi_db_wait_duration_seconds",
			Help: "Database wait duration in seconds from sql.DB stats",
		},
	)

	redisPoolConnections = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "newapi_redis_pool_connections",
			Help: "Redis connection pool stats",
		},
		[]string{"state"},
	)

	redisPoolEvents = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "newapi_redis_pool_events",
			Help: "Redis pool cumulative events",
		},
		[]string{"event"},
	)

	channelTestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "newapi_channel_test_total",
			Help: "Total number of channel test requests",
		},
		[]string{"trigger", "channel_type", "channel_id", "channel_name", "result"},
	)

	channelTestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "newapi_channel_test_duration_seconds",
			Help:    "Channel test duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"trigger", "channel_type", "channel_id", "channel_name", "result"},
	)
)

func Init() {
	registerMetrics()
	startDependencyProbe()
}

func registerMetrics() {
	registerOnce.Do(func() {
		registerCollector(collectors.NewGoCollector())
		registerCollector(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		registerCollector(httpRequestsTotal)
		registerCollector(httpRequestDuration)
		registerCollector(httpInFlight)
		registerCollector(relayRequestsTotal)
		registerCollector(relayRequestDuration)
		registerCollector(relayErrorsTotal)
		registerCollector(dependencyEnabled)
		registerCollector(dependencyUp)
		registerCollector(dbConnections)
		registerCollector(dbWaitTotal)
		registerCollector(dbWaitDuration)
		registerCollector(redisPoolConnections)
		registerCollector(redisPoolEvents)
		registerCollector(channelTestTotal)
		registerCollector(channelTestDuration)
	})
}

func ObserveChannelTest(trigger string, channelType int, channelID int, channelName string, success bool, durationSeconds float64) {
	if trigger == "" {
		trigger = "manual"
	}
	result := "failure"
	if success {
		result = "success"
	}
	channelTypeName := constant.GetChannelTypeName(channelType)
	channelIDStr := strconv.Itoa(channelID)
	channelTestTotal.WithLabelValues(trigger, channelTypeName, channelIDStr, channelName, result).Inc()
	channelTestDuration.WithLabelValues(trigger, channelTypeName, channelIDStr, channelName, result).Observe(durationSeconds)
}

func registerCollector(collector prometheus.Collector) {
	err := prometheus.Register(collector)
	if err == nil {
		return
	}
	if _, ok := err.(prometheus.AlreadyRegisteredError); ok {
		return
	}
	panic(err)
}

func PrometheusHandler() gin.HandlerFunc {
	handler := promhttp.Handler()
	metricsToken := strings.TrimSpace(common.GetEnvOrDefaultString("METRICS_TOKEN", ""))

	return func(c *gin.Context) {
		c.Set(routeTagKey, "metrics")
		if metricsToken != "" {
			token := extractToken(c)
			if token != metricsToken {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
		}
		handler.ServeHTTP(c.Writer, c.Request)
	}
}

func HTTPMetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		inFlightTag := c.GetString(routeTagKey)
		if inFlightTag == "" {
			inFlightTag = "web"
		}

		httpInFlight.WithLabelValues(inFlightTag).Inc()
		start := time.Now()
		c.Next()

		tag := c.GetString(routeTagKey)
		if tag == "" {
			tag = "web"
		}

		latency := time.Since(start).Seconds()
		statusValue := c.Writer.Status()
		statusCode := strconv.Itoa(statusValue)
		statusClass := statusClass(statusValue)
		errorReason := normalizeRelayErrorReason(common.GetContextKeyString(c, constant.ContextKeyRelayErrorReason))
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		httpInFlight.WithLabelValues(inFlightTag).Dec()
		httpRequestsTotal.WithLabelValues(tag, c.Request.Method, route, statusCode, statusClass, errorReason).Inc()
		httpRequestDuration.WithLabelValues(tag, c.Request.Method, route).Observe(latency)

		if tag == "relay" {
			channelType := constant.GetChannelTypeName(common.GetContextKeyInt(c, constant.ContextKeyChannelType))
			channelID := strconv.Itoa(common.GetContextKeyInt(c, constant.ContextKeyChannelId))
			channelName := common.GetContextKeyString(c, constant.ContextKeyChannelName)

			relayRequestsTotal.WithLabelValues(channelType, channelID, channelName, statusClass).Inc()
			relayRequestDuration.WithLabelValues(channelType, channelID, channelName).Observe(latency)
			if statusValue >= 500 {
				relayErrorsTotal.WithLabelValues(channelType, channelID, channelName).Inc()
			}
		}
	}
}

func normalizeRelayErrorReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "none"
	}
	if _, ok := relayErrorReasonWhitelist[reason]; ok {
		return reason
	}
	return "token_auth_failed"
}

func statusClass(statusCode int) string {
	switch {
	case statusCode >= 500:
		return "5xx"
	case statusCode >= 400:
		return "4xx"
	case statusCode >= 300:
		return "3xx"
	case statusCode >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}

func extractToken(c *gin.Context) string {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		return strings.TrimSpace(authorization[7:])
	}
	token := strings.TrimSpace(c.GetHeader("X-Metrics-Token"))
	if token != "" {
		return token
	}
	return strings.TrimSpace(c.Query("token"))
}

func startDependencyProbe() {
	probeOnce.Do(func() {
		collectDependencyMetrics()
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				collectDependencyMetrics()
			}
		}()
	})
}

func collectDependencyMetrics() {
	collectDatabaseMetrics()
	collectRedisMetrics()
}

func collectDatabaseMetrics() {
	dependencyEnabled.WithLabelValues("database").Set(1)
	if err := model.PingDB(); err != nil {
		dependencyUp.WithLabelValues("database").Set(0)
		return
	}
	dependencyUp.WithLabelValues("database").Set(1)

	sqlDB, err := model.DB.DB()
	if err != nil {
		return
	}
	stats := sqlDB.Stats()
	dbConnections.WithLabelValues("open").Set(float64(stats.OpenConnections))
	dbConnections.WithLabelValues("in_use").Set(float64(stats.InUse))
	dbConnections.WithLabelValues("idle").Set(float64(stats.Idle))
	dbConnections.WithLabelValues("max_open").Set(float64(stats.MaxOpenConnections))
	dbWaitTotal.Set(float64(stats.WaitCount))
	dbWaitDuration.Set(stats.WaitDuration.Seconds())
}

func collectRedisMetrics() {
	if !common.RedisEnabled || common.RDB == nil {
		dependencyEnabled.WithLabelValues("redis").Set(0)
		dependencyUp.WithLabelValues("redis").Set(0)
		return
	}
	dependencyEnabled.WithLabelValues("redis").Set(1)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := common.RDB.Ping(ctx).Result(); err != nil {
		dependencyUp.WithLabelValues("redis").Set(0)
		return
	}
	dependencyUp.WithLabelValues("redis").Set(1)

	stats := common.RDB.PoolStats()
	redisPoolConnections.WithLabelValues("total").Set(float64(stats.TotalConns))
	redisPoolConnections.WithLabelValues("idle").Set(float64(stats.IdleConns))
	redisPoolConnections.WithLabelValues("stale").Set(float64(stats.StaleConns))
	redisPoolEvents.WithLabelValues("hits").Set(float64(stats.Hits))
	redisPoolEvents.WithLabelValues("misses").Set(float64(stats.Misses))
	redisPoolEvents.WithLabelValues("timeouts").Set(float64(stats.Timeouts))
}
