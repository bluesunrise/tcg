package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/contrib/cors"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gwos/tcg/cache"
	"github.com/gwos/tcg/config"
	"github.com/gwos/tcg/log"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/swaggo/gin-swagger/swaggerFiles"
	"go.opentelemetry.io/otel/label"
)

// Controller implements AgentServices, Controllers interface
type Controller struct {
	*TransitService
	entrypoints []Entrypoint
	srv         *http.Server
}

// Entrypoint describes controller API
type Entrypoint struct {
	URL     string
	Method  string
	Handler func(c *gin.Context)
}

const startTimeout = time.Millisecond * 200
const shutdownTimeout = time.Millisecond * 200

var onceController sync.Once
var controller *Controller

// GetController implements Singleton pattern
func GetController() *Controller {
	onceController.Do(func() {
		controller = &Controller{
			GetTransitService(),
			[]Entrypoint{},
			nil,
		}
	})
	return controller
}

// RegisterEntrypoints sets addition API
func (controller *Controller) RegisterEntrypoints(entrypoints []Entrypoint) {
	controller.entrypoints = entrypoints
}

// RemoveEntrypoints removes addition API
func (controller *Controller) RemoveEntrypoints() {
	controller.entrypoints = []Entrypoint{}
}

// starts the http server
// overrides AgentService implementation
func (controller *Controller) startController() error {
	if controller.srv != nil {
		log.Warn("StartController: already started")
		return nil
	}

	var addr string
	if strings.HasPrefix(controller.Connector.ControllerAddr, ":") {
		addr = "localhost" + controller.Connector.ControllerAddr
	} else {
		addr = controller.Connector.ControllerAddr
	}
	certFile := controller.Connector.ControllerCertFile
	keyFile := controller.Connector.ControllerKeyFile

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowedHeaders = []string{"GWOS-APP-NAME", "GWOS-API-TOKEN", "Content-Type"}
	router.Use(cors.New(corsConfig))
	router.Use(sessions.Sessions("tcg-session", sessions.NewCookieStore([]byte("secret"))))
	controller.registerAPI1(router, addr, controller.entrypoints)

	controller.srv = &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  controller.Connector.ControllerReadTimeout,
		WriteTimeout: controller.Connector.ControllerWriteTimeout,
	}

	go func() {
		controller.agentStatus.Controller = Running
		if certFile != "" && keyFile != "" {
			log.Info("[Controller]: Start listen TLS: ", addr)
			if err := controller.srv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				log.Error("[Controller]: http.Server error: ", err)
			}
		} else {
			log.Info("[Controller]: Start listen: ", addr)
			if err := controller.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("[Controller]: http.Server error: ", err)
			}
		}
		/* getting here after http.Server exit */
		controller.srv = nil
		controller.agentStatus.Controller = Stopped
	}()
	// TODO: ensure signal processing in case of linked library
	// // Wait for interrupt signal to gracefully shutdown the server
	// quit := make(chan os.Signal)
	// // kill (no param) default send syscall.SIGTERM
	// // kill -2 is syscall.SIGINT
	// // kill -9 is syscall.SIGKILL but can't be catch, so don't need add it
	// signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	// <-quit
	// StopServer()

	// prevent misbehavior on immediate shutdown
	time.Sleep(startTimeout)
	return nil
}

// gracefully shutdowns the http server
// overrides AgentService implementation
func (controller *Controller) stopController() error {
	// NOTE: the controller.agentStatus.Controller will be updated by controller.StartController itself
	log.Info("[Controller]: Shutdown ...")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if controller.srv != nil {
		if err := controller.srv.Shutdown(ctx); err != nil {
			log.Warn("[Controller]: Shutdown:", err)
		}
	}
	log.Info("[Controller]: Exiting")
	<-ctx.Done()
	return nil
}

//
// @Description The following API endpoint can be used to Agent configure.
// @Tags    agent, connector
// @Accept  json
// @Produce json
// @Success 200
// @Failure 401 {string} string "Unauthorized"
// @Router  /config [post]
// @Param   GWOS-APP-NAME    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) config(c *gin.Context) {
	payload, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, err.Error())
		return
	}
	var dto config.ConnectorDTO
	if err := json.Unmarshal(payload, &dto); err != nil {
		log.Error("|controller.go| : [config] : ", err)
		c.JSON(http.StatusBadRequest, "unmarshal connector dto")
		return
	}

	credentials := cache.Credentials{
		GwosAppName:  c.Request.Header.Get("GWOS-APP-NAME"),
		GwosAPIToken: c.Request.Header.Get("GWOS-API-TOKEN"),
	}
	err = controller.dsClient.ValidateToken(credentials.GwosAppName, credentials.GwosAPIToken, dto.DSConnection.HostName)
	if err != nil {
		c.JSON(http.StatusBadRequest, fmt.Sprintf("Couldn't validate config token request: %s", dto.DSConnection.HostName))
	}

	_, _ = controller.ctrlPushAsync(payload, ctrlSubjConfig, nil)
	c.JSON(http.StatusOK, nil)
}

//
// @Description The following API endpoint can be used to send Alerts to Foundation.
// @Tags    alert, event
// @Accept  json
// @Produce json
// @Success 200
// @Failure 401 {string} string "Unauthorized"
// @Router  /events [post]
// @Param   GWOS-APP-NAME    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) events(c *gin.Context) {
	var (
		err     error
		payload []byte
	)
	ctx, span := StartTraceSpan(context.Background(), "services", "eventsUnack")
	defer func() {
		span.SetAttributes(
			label.Int("payloadLen", len(payload)),
			label.String("error", fmt.Sprint(err)),
			label.String("entrypoint", c.FullPath()),
		)
		span.End()
	}()

	payload, err = c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, err.Error())
		return
	}
	err = controller.SendEvents(ctx, payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, nil)
}

//
// @Description The following API endpoint can be used to send Alerts to Foundation.
// @Tags    alert, event
// @Accept  json
// @Produce json
// @Success 200
// @Failure 401 {string} string "Unauthorized"
// @Router  /events-ack [post]
// @Param   GWOS-APP-NAME    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) eventsAck(c *gin.Context) {
	var (
		err     error
		payload []byte
	)
	ctx, span := StartTraceSpan(context.Background(), "services", "eventsUnack")
	defer func() {
		span.SetAttributes(
			label.Int("payloadLen", len(payload)),
			label.String("error", fmt.Sprint(err)),
			label.String("entrypoint", c.FullPath()),
		)
		span.End()
	}()

	payload, err = c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, err.Error())
		return
	}
	err = controller.SendEventsAck(ctx, payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, nil)
}

//
// @Description The following API endpoint can be used to send Alerts to Foundation.
// @Tags    alert, event
// @Accept  json
// @Produce json
// @Success 200
// @Failure 401 {string} string "Unauthorized"
// @Router  /events-unack [post]
// @Param   GWOS-APP-NAME    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) eventsUnack(c *gin.Context) {
	var (
		err     error
		payload []byte
	)
	ctx, span := StartTraceSpan(context.Background(), "services", "eventsUnack")
	defer func() {
		span.SetAttributes(
			label.Int("payloadLen", len(payload)),
			label.String("error", fmt.Sprint(err)),
			label.String("entrypoint", c.FullPath()),
		)
		span.End()
	}()

	payload, err = c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, err.Error())
		return
	}
	err = controller.SendEventsUnack(ctx, payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, nil)
}

//
// @Description The following API endpoint can be used to get list of metrics from the server.
// @Tags    metric
// @Accept  json
// @Produce json
// @Success 200 {object} services.AgentStatus
// @Failure 401 {string} string "Unauthorized"
// @Failure 500 {string} string "Internal server error"
// @Router  /metrics [get]
// @Param   GWOS-APP-NAME    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) listMetrics(c *gin.Context) {
	metrics, err := controller.ListMetrics()
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}
	c.Data(http.StatusOK, gin.MIMEJSON, metrics)
}

//
// @Description The following API endpoint can be used to start NATS dispatcher.
// @Tags    agent, connector
// @Accept  json
// @Produce json
// @Success 200 {object} services.ConnectorStatusDTO
// @Failure 401 {string} string "Unauthorized"
// @Failure 500 {string} string "Internal server error"
// @Router  /start [post]
// @Param   GWOS-APP-NAME    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) start(c *gin.Context) {
	status := controller.Status()
	if status.Transport == Running && status.Ctrl == nil {
		c.JSON(http.StatusOK, ConnectorStatusDTO{Running, 0})
		return
	}
	ctrl, err := controller.StartTransportAsync(nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, ConnectorStatusDTO{Processing, ctrl.Idx})
}

//
// @Description The following API endpoint can be used to stop NATS dispatcher.
// @Tags    agent, connector
// @Accept  json
// @Produce json
// @Success 200 {object} services.ConnectorStatusDTO
// @Failure 401 {string} string "Unauthorized"
// @Failure 500 {string} string "Internal server error"
// @Router  /stop [post]
// @Param   GWOS-APP-NAME    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) stop(c *gin.Context) {
	status := controller.Status()
	if status.Transport == Stopped && status.Ctrl == nil {
		c.JSON(http.StatusOK, ConnectorStatusDTO{Stopped, 0})
		return
	}
	ctrl, err := controller.StopTransportAsync(nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, ConnectorStatusDTO{Processing, ctrl.Idx})
}

//
// @Description The following API endpoint can be used to get TCG statistics.
// @Tags    agent, connector
// @Accept  json
// @Produce json
// @Success 200 {object} services.AgentIdentityStats
// @Failure 401 {string} string "Unauthorized"
// @Router  /stats [get]
// @Param   gwos-app-name    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) stats(c *gin.Context) {
	c.JSON(http.StatusOK, controller.Stats())
}

//
// @Description The following API endpoint can be used to get a TCG agent id
// @Tags    agent, connector
// @Accept  json
// @Produce json
// @Success 200 {object} services.AgentIdentity
// @Router  /agent [get]
func (controller *Controller) agentIdentity(c *gin.Context) {
	agentIdentity := AgentIdentity{
		AgentID: controller.Connector.AgentID,
		AppName: controller.Connector.AppName,
		AppType: controller.Connector.AppType,
	}
	c.JSON(http.StatusOK, agentIdentity)
}

//
// @Description The following API endpoint can be used to get TCG status.
// @Tags    agent, connector
// @Accept  json
// @Produce json
// @Success 200 {object} services.ConnectorStatusDTO
// @Failure 401 {string} string "Unauthorized"
// @Router  /status [get]
// @Param   GWOS-APP-NAME    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) status(c *gin.Context) {
	status := controller.Status()
	statusDTO := ConnectorStatusDTO{status.Transport, 0}
	if status.Ctrl != nil {
		statusDTO = ConnectorStatusDTO{Processing, status.Ctrl.Idx}
	}
	c.JSON(http.StatusOK, statusDTO)
}

//
// @Description The following API endpoint can be used to return actual TCG connector version.
// @Tags    agent, connector
// @Accept  json
// @Produce json
// @Success 200 {object} config.BuildVersion
// @Failure 401 {string} string "Unauthorized"
// @Router  /version [get]
// @Param   GWOS-APP-NAME    header    string     true        "Auth header"
// @Param   GWOS-API-TOKEN   header    string     true        "Auth header"
func (controller *Controller) version(c *gin.Context) {
	c.JSON(http.StatusOK, config.GetBuildInfo())
}

func (controller *Controller) validateToken(c *gin.Context) {
	// check local pin
	pin := controller.Connector.ControllerPin
	if len(pin) > 0 && pin == c.Request.Header.Get("X-PIN") {
		return
	}

	credentials := cache.Credentials{
		GwosAppName:  c.Request.Header.Get("GWOS-APP-NAME"),
		GwosAPIToken: c.Request.Header.Get("GWOS-API-TOKEN"),
	}

	if credentials.GwosAppName == "" || credentials.GwosAPIToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": `Invalid GWOS-APP-NAME or GWOS-API-TOKEN`})
		c.Abort()
		return
	}

	key := fmt.Sprintf("%s:%s", credentials.GwosAppName, credentials.GwosAPIToken)

	_, isCached := cache.AuthCache.Get(key)
	if !isCached {
		err := controller.dsClient.ValidateToken(credentials.GwosAppName, credentials.GwosAPIToken, "")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		err = cache.AuthCache.Add(key, credentials, 8*time.Hour)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
}

func (controller *Controller) registerAPI1(router *gin.Engine, addr string, entrypoints []Entrypoint) {
	swaggerURL := ginSwagger.URL("http://" + addr + "/swagger/doc.json")
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, swaggerURL))

	apiV1Group := router.Group("/api/v1")
	apiV1Config := router.Group("/api/v1/config")
	apiV1Identity := router.Group("/api/v1/identity")
	apiV1Group.Use(controller.validateToken)

	apiV1Config.POST("", controller.config)
	apiV1Group.GET("/version", controller.version)
	apiV1Group.POST("/events", controller.events)
	apiV1Group.POST("/events-ack", controller.eventsAck)
	apiV1Group.POST("/events-unack", controller.eventsUnack)
	apiV1Group.GET("/metrics", controller.listMetrics)
	apiV1Group.POST("/start", controller.start)
	apiV1Group.POST("/stop", controller.stop)
	apiV1Group.GET("/stats", controller.stats)
	apiV1Group.GET("/status", controller.status)
	apiV1Identity.GET("", controller.agentIdentity)

	for _, entrypoint := range entrypoints {
		switch entrypoint.Method {
		case http.MethodGet:
			apiV1Group.GET(entrypoint.URL, entrypoint.Handler)
		case http.MethodPost:
			apiV1Group.POST(entrypoint.URL, entrypoint.Handler)
		case http.MethodPut:
			apiV1Group.PUT(entrypoint.URL, entrypoint.Handler)
		case http.MethodDelete:
			apiV1Group.DELETE(entrypoint.URL, entrypoint.Handler)
		}
	}

	pprofGroup := apiV1Group.Group("/debug/pprof")
	pprofGroup.GET("/", gin.WrapF(pprof.Index))
	pprofGroup.GET("/cmdline", gin.WrapF(pprof.Cmdline))
	pprofGroup.GET("/profile", gin.WrapF(pprof.Profile))
	pprofGroup.GET("/symbol", gin.WrapF(pprof.Symbol))
	pprofGroup.POST("/symbol", gin.WrapF(pprof.Symbol))
	pprofGroup.GET("/trace", gin.WrapF(pprof.Trace))
	pprofGroup.GET("/allocs", gin.WrapF(pprof.Handler("allocs").ServeHTTP))
	pprofGroup.GET("/block", gin.WrapF(pprof.Handler("block").ServeHTTP))
	pprofGroup.GET("/goroutine", gin.WrapF(pprof.Handler("goroutine").ServeHTTP))
	pprofGroup.GET("/heap", gin.WrapF(pprof.Handler("heap").ServeHTTP))
	pprofGroup.GET("/mutex", gin.WrapF(pprof.Handler("mutex").ServeHTTP))
	pprofGroup.GET("/threadcreate", gin.WrapF(pprof.Handler("threadcreate").ServeHTTP))
}
