package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var myTemplates = NewTemplates()

type Templates struct {
	templates *template.Template
}

func (t *Templates) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func renderTemplateToString(name string, data interface{}) string {
	var buf bytes.Buffer
	err := myTemplates.templates.ExecuteTemplate(&buf, name, data)
	if err != nil {
		log.Fatal(err)
	}
	inline := bytes.ReplaceAll(buf.Bytes(), []byte("\n"), []byte(""))
	return string(inline)
}

func NewTemplates() *Templates {
	return &Templates{
		templates: template.Must(template.ParseGlob("views/*.html")),
	}
}

func NewTimeTickEvent() Event {
	// elapsedFromStartedAt := time.Since(appState.CurrentPomo.StartedAt)
	remaining := appState.PreferredDuration
	if appState.CurrentPomo != nil {
		remaining = appState.CurrentPomo.Duration - time.Second*time.Duration(appState.CurrentPomo.Ticks)
	}
	if remaining <= 0 {
		appState.Running = false
		remaining = 0
	}
	return Event{
		Event: []byte("TimeTick"),
		Data:  []byte(fmt.Sprintf("%02d:%02d", int(remaining.Minutes()), int(remaining.Seconds())%60)),
	}
}

type AppState struct {
	CurrentPomo       *PomodoroState
	PreferredDuration time.Duration
	Clients           map[chan Event]struct{}
	Running           bool
}

func (s AppState) PrintPreferredDuration() string {
	return durationToStr(s.PreferredDuration)
}

type PomodoroState struct {
	StartedAt time.Time
	Duration  time.Duration
	Ticks     int
	FlowMode  bool // ignore Duration and act like stopwatch
}

func (p *PomodoroState) TimeLeft() string {
	remaining := appState.PreferredDuration
	if appState.CurrentPomo != nil {
		remaining = appState.CurrentPomo.Duration - time.Second*time.Duration(appState.CurrentPomo.Ticks)
	}
	if remaining <= 0 {
		appState.Running = false
		remaining = 0
	}
	return fmt.Sprintf("%02d:%02d", int(remaining.Minutes()), int(remaining.Seconds())%60)
}

func durationToStr(d time.Duration) string {
	return fmt.Sprintf("%02d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

var appState = AppState{
	CurrentPomo:       nil,
	PreferredDuration: 25 * time.Minute,
	Clients:           make(map[chan Event]struct{}),
	Running:           false,
}

func main() {
	e := echo.New()
	e.Renderer = myTemplates
	e.Use(middleware.Logger())

	e.Static("/images", "images")
	e.Static("/css", "css")
	e.Static("/js", "js")

	e.GET("/", func(c echo.Context) error {
		return c.Render(http.StatusOK, "index", appState)
	})
	e.POST("/start-pause", handleStartPause)
	e.POST("/reset", handleReset)
	e.GET("/events", sseHandler)

	e.GET("/debug", func(c echo.Context) error {
		return c.Render(http.StatusOK, "debug", appState)
	})

	e.GET("/client-count", func(c echo.Context) error {
		return c.String(http.StatusOK, fmt.Sprintf("Clients: %d", len(appState.Clients)))
	})

	go broadcaster()

	e.Logger.Fatal(e.Start(":1323"))
}

func handleStartPause(c echo.Context) error {
	EventName := ""
	if !appState.Running && appState.CurrentPomo == nil {
		appState.CurrentPomo = &PomodoroState{
			StartedAt: time.Now().UTC(),
			Duration:  appState.PreferredDuration,
			Ticks:     0,
		}
		appState.Running = true
		EventName = "PomodoroStarted"
	} else if appState.Running && appState.CurrentPomo != nil {
		appState.Running = false
		EventName = "PomodoroPaused"
	} else if !appState.Running && appState.CurrentPomo != nil {
		appState.Running = true
		EventName = "PomodoroResumed"
	}

	go broadcastEvent(Event{
		Event: []byte(EventName),
		Data:  []byte(renderTemplateToString("controls", appState)),
	})

	return c.NoContent(http.StatusNoContent)
}

func broadcastEvent(e Event) {
	for ch := range appState.Clients {
		select {
		case ch <- e:
		default:
		}
	}
}

func handleReset(c echo.Context) error {
	appState.Running = false
	appState.CurrentPomo = nil
	go broadcastEvent(Event{
		Event: []byte("PomodoroReset"),
		Data:  []byte(renderTemplateToString("controls", appState)),
	})
	go broadcastEvent(NewTimeTickEvent())

	return c.NoContent(http.StatusNoContent)
}

func sseHandler(c echo.Context) error {
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")

	ch := make(chan Event, 100)
	appState.Clients[ch] = struct{}{}

	notify := c.Request().Context().Done()

	// send whole state on initial connect or reconnect
	syncWhole(ch)

	go func() {
		for {
			select {
			case <-notify:
				delete(appState.Clients, ch)
				close(ch)
				return
			case e := <-ch:
				err := e.MarshalTo(c.Response())
				if err != nil {
					delete(appState.Clients, ch)
					close(ch)
					return
				}
				c.Response().Flush()
			}
		}
	}()

	<-notify
	return nil
}

func syncWhole(ch chan Event) {
	ch <- Event{
		Event: []byte("SyncControls"),
		Data:  []byte(renderTemplateToString("controls", appState)),
	}
	ch <- Event{
		Event: []byte("SyncTimer"),
		Data:  []byte(renderTemplateToString("timer", appState)),
	}
}

func broadcaster() {
	ticker := time.NewTicker(1 * time.Second)
	ping := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-ticker.C:
			if !appState.Running {
				continue
			}
			appState.CurrentPomo.Ticks++
			go broadcastEvent(NewTimeTickEvent())
		case <-ping.C:
			broadcastEvent(Event{Event: []byte("Ping"), Data: []byte("keepalive")})

		}
	}
}
