package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
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
	funcMap := template.FuncMap{
		"toJson": func(v interface{}) template.JS {
			b, err := json.Marshal(v)
			if err != nil {
				return ""
			}
			return template.JS(b) // This avoids escaping quotes in JS.
		},
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob("views/*.html"))
	return &Templates{
		templates: tmpl,
	}
}

func SyncTimerEvent() Event {
	return Event{
		Event: []byte("SyncTimer"),
		Data:  []byte(renderTemplateToString("timer", appState)),
	}
}

type AppState struct {
	CurrentPomo       *Pomodoro
	PreferredDuration time.Duration
	Clients           map[chan Event]struct{}
	Tracked           []Pomodoro
	Activities        map[int]Activity
	CurrentActivity   Activity
	FlowMode          bool
	AutoBreak         bool
	Running           bool
	DailyGoal         int
}

type Activity struct {
	Id   int
	Name string
}

func (s AppState) PrintPreferredDuration() string {
	if appState.FlowMode {
		return DurationToStr(0)
	}
	return DurationToStr(s.PreferredDuration)
}

func (s AppState) TotalTicks() int {
	totalTicks := 0
	for _, p := range appState.Tracked {
		totalTicks += p.Ticks
	}
	return totalTicks
}

func (s AppState) PrintTotalPomos() string {
	completedPomos := (time.Second * time.Duration(appState.TotalTicks())).Minutes()
	completedPomos = completedPomos / (25 * time.Minute).Minutes()
	return fmt.Sprintf("%0.1f", completedPomos)
}
func (s AppState) PrintTotalTime() string {
	return DurationToStr(time.Second * time.Duration(s.TotalTicks()))
}

func (s AppState) PreferredDurationMinutes() int {
	return int(s.PreferredDuration.Minutes())
}

const shortBreakDuration = 5 * time.Minute
const longBreakDuration = 15 * time.Minute

type Pomodoro struct {
	StartedAt time.Time
	Duration  time.Duration
	Activity  Activity
	FlowMode  bool // ignore Duration and act like stopwatch
	IsBreak   bool // Duration as break time
	Ticks     int
}

func (p *Pomodoro) Completed() bool {
	return time.Second*time.Duration(p.Ticks) >= p.Duration
}

func (p *Pomodoro) PrintTime() string {
	remaining := appState.PreferredDuration
	if appState.CurrentPomo != nil {
		remaining = appState.CurrentPomo.Duration - time.Second*time.Duration(appState.CurrentPomo.Ticks)
	}
	if appState.FlowMode && !p.IsBreak {
		remaining = time.Second * 0
		if appState.CurrentPomo != nil {
			remaining = time.Second * time.Duration(appState.CurrentPomo.Ticks)
		}
	}

	return DurationToStr(remaining)
}

func (p *Pomodoro) TimePassed() string {
	t := time.Second * time.Duration(p.Ticks)
	return DurationToStr(t)
}

func DurationToStr(d time.Duration) string {
	neg := d < 0
	if neg {
		d = -d
	}

	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	s := int((d % time.Minute) / time.Second)

	var result string
	if h > 0 {
		result = fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	} else {
		result = fmt.Sprintf("%02d:%02d", m, s)
	}

	if neg {
		return "-" + result
	}
	return result
}

var activities = map[int]Activity{
	1: {
		Id:   1,
		Name: "General",
	},
	2: {
		Id:   2,
		Name: "Boot.dev",
	},
	3: {
		Id:   3,
		Name: "Personal projects",
	},
	4: {
		Id:   4,
		Name: "Japanese",
	},
}

var appState = AppState{
	CurrentPomo:       nil,
	PreferredDuration: 25 * time.Minute,
	Clients:           make(map[chan Event]struct{}),
	Running:           false,
	FlowMode:          false,
	AutoBreak:         true,
	Tracked: []Pomodoro{{
		StartedAt: time.Now().UTC(),
		Duration:  25 * time.Minute,
		Ticks:     300,
		FlowMode:  false,
		Activity:  activities[1],
	}, {
		StartedAt: time.Now().UTC().Add(5 * time.Minute),
		Duration:  25 * time.Minute,
		Ticks:     900,
		FlowMode:  false,
		Activity:  activities[2],
	}},
	DailyGoal:       8,
	CurrentActivity: activities[1],
	Activities:      activities,
}

var clientsMu sync.RWMutex

func main() {
	e := echo.New()
	e.Renderer = myTemplates

	// e.Use(middleware.Logger())
	e.Use(middleware.LoggerWithConfig(middleware.DefaultLoggerConfig))
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(rate.Limit(20))))

	e.Static("/images", "images")
	e.Static("/css", "css")
	e.Static("/js", "js")

	e.GET("/", func(c echo.Context) error {
		return c.Render(http.StatusOK, "index", appState)
	})
	demoPomos := generateDemoPomodoros()
	e.GET("/stats", func(c echo.Context) error {
		return c.Render(http.StatusOK, "stats", Stats{
			AppState: appState,
			Stats:    demoPomos,
			EfficiencyStats: []EfficiencyStats{
				CalculateEfficiency(7, demoPomos),
				CalculateEfficiency(14, demoPomos),
				CalculateEfficiency(30, demoPomos),
				CalculateEfficiency(60, demoPomos),
				CalculateEfficiency(90, demoPomos),
				CalculateEfficiency(120, demoPomos),
				CalculateEfficiency(180, demoPomos),
			},
			DailyGoal:  appState.DailyGoal,
			DailyStats: CalculateDailyStats(demoPomos),
		})
	})
	// controls
	e.POST("/start-pause", handleStartPause)
	e.POST("/reset", handleReset)
	e.POST("/save", handleSave)
	e.POST("/activity", handleActivityChange)
	e.POST("/toggle-flow", handleToggleFlow)
	e.POST("/toggle-auto-break", handleToggleAutoBreak)
	e.POST("/set-duration", handleDuration)
	e.POST("/skip-break", handleReset)
	e.POST("/start-break", handleStartBreak)

	// [ ] when http 1.1 used browser limits connections to 6 tabs
	e.GET("/events", sseHandler)

	e.GET("/debug", func(c echo.Context) error {
		return c.Render(http.StatusOK, "debug", appState)
	})

	e.GET("/clients", func(c echo.Context) error {
		return c.String(http.StatusOK, fmt.Sprintf("Clients: %d", len(appState.Clients)))
	})

	go broadcaster()

	e.Logger.Fatal(e.Start(":1323"))
}

func handleStartBreak(c echo.Context) error {
	// appState.AutoBreak = !appState.AutoBreak
	// go broadcastEvent(Event{
	// 	Event: []byte("SyncMode"),
	// 	Data:  []byte(renderTemplateToString("mode", appState)),
	// })
	// TODO: validation :)

	pomo := appState.CurrentPomo
	savePomo()
	startBreak(*pomo)
	go broadcastEvent(Event{
		Event: []byte("SyncControls"),
		Data:  []byte(renderTemplateToString("controls", appState)),
	})
	return c.NoContent(http.StatusNoContent)
}

func handleToggleAutoBreak(c echo.Context) error {
	appState.AutoBreak = !appState.AutoBreak
	go broadcastEvent(Event{
		Event: []byte("SyncMode"),
		Data:  []byte(renderTemplateToString("mode", appState)),
	})
	return c.NoContent(http.StatusNoContent)
}

func handleToggleFlow(c echo.Context) error {
	appState.FlowMode = !appState.FlowMode
	if appState.CurrentPomo != nil && !appState.CurrentPomo.IsBreak {
		appState.CurrentPomo.FlowMode = appState.FlowMode
	}
	go broadcastEvent(SyncTimerEvent())
	go broadcastEvent(Event{
		Event: []byte("SyncMode"),
		Data:  []byte(renderTemplateToString("mode", appState)),
	})
	go broadcastEvent(Event{
		Event: []byte("SyncControls"),
		Data:  []byte(renderTemplateToString("controls", appState)),
	})
	return c.NoContent(http.StatusNoContent)
}

func handleDuration(c echo.Context) error {
	newDuration, _ := strconv.Atoi(c.FormValue("duration"))
	appState.PreferredDuration = time.Minute * time.Duration(newDuration)
	if appState.CurrentPomo != nil && !appState.CurrentPomo.IsBreak {
		appState.CurrentPomo.Duration = appState.PreferredDuration
	}
	go broadcastEvent(SyncTimerEvent())
	go broadcastEvent(Event{
		Event: []byte("SyncMode"),
		Data:  []byte(renderTemplateToString("mode", appState)),
	})
	return c.NoContent(http.StatusNoContent)
}

func handleActivityChange(c echo.Context) error {
	activity := appState.CurrentActivity
	receivedId, _ := strconv.Atoi(c.FormValue("activity"))
	if newActivity, ok := appState.Activities[receivedId]; ok {
		activity = newActivity
	}

	if appState.CurrentPomo != nil && !appState.CurrentPomo.IsBreak {
		appState.CurrentPomo.Activity = activity
	}
	appState.CurrentActivity = activity

	go broadcastEvent(Event{
		Event: []byte("SyncActivities"),
		Data:  []byte(renderTemplateToString("activities", appState)),
	})
	return c.NoContent(http.StatusNoContent)
}

func handleStartPause(c echo.Context) error {
	if appState.CurrentPomo != nil && appState.CurrentPomo.IsBreak {
		return nil
	}
	EventName := ""
	if !appState.Running && appState.CurrentPomo == nil {
		appState.CurrentPomo = &Pomodoro{
			StartedAt: time.Now().UTC(),
			Duration:  appState.PreferredDuration,
			Ticks:     0,
			FlowMode:  appState.FlowMode,
			Activity:  appState.CurrentActivity,
			IsBreak:   false,
		}
		appState.Running = true
		EventName = "PomoStarted"
	} else if appState.Running && appState.CurrentPomo != nil {
		appState.Running = false
		EventName = "PomoPaused"
	} else if !appState.Running && appState.CurrentPomo != nil {
		appState.Running = true
		EventName = "PomoResumed"
	}

	go broadcastEvent(Event{
		Event: []byte(EventName),
		Data:  []byte(renderTemplateToString("controls", appState)),
	})

	return c.NoContent(http.StatusNoContent)
}

func handleSave(c echo.Context) error {
	if appState.CurrentPomo == nil || appState.CurrentPomo.IsBreak {
		return c.NoContent(http.StatusBadRequest)
	}

	savePomo()

	go broadcastEvent(Event{
		Event: []byte("PomoSaved"),
		Data:  []byte(renderTemplateToString("controls", appState)),
	})

	go broadcastEvent(SyncTimerEvent())

	return c.NoContent(http.StatusNoContent)
}

func savePomo() {
	appState.Running = false
	appState.Tracked = append(appState.Tracked, *appState.CurrentPomo)
	appState.CurrentPomo = nil
	go broadcastEvent(Event{
		Event: []byte("SyncTracked"),
		Data:  []byte(renderTemplateToString("tracked", appState)),
	})
}

func broadcastEvent(e Event) {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	for ch := range appState.Clients {
		select {
		case ch <- e:
		default:
			close(ch)
			clientsMu.Lock()
			delete(appState.Clients, ch)
			clientsMu.Unlock()
		}
	}
}

func handleReset(c echo.Context) error {
	appState.Running = false
	appState.CurrentPomo = nil
	go broadcastEvent(Event{
		Event: []byte("PomoReset"),
		Data:  []byte(renderTemplateToString("controls", appState)),
	})
	go broadcastEvent(SyncTimerEvent())

	return c.NoContent(http.StatusNoContent)
}

func sseHandler(c echo.Context) error {
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")

	ch := make(chan Event, 100)
	clientsMu.Lock()
	appState.Clients[ch] = struct{}{}
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(appState.Clients, ch)
		clientsMu.Unlock()
		close(ch)
	}()

	// send whole state on initial connect or reconnect
	syncWhole(ch)

	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case e := <-ch:
			err := e.MarshalTo(c.Response())
			if err != nil {
				return err
			}
			c.Response().Flush()
		}
	}
}

func syncWhole(ch chan Event) {
	ch <- Event{
		Event: []byte("SyncControls"),
		Data:  []byte(renderTemplateToString("controls", appState)),
	}
	ch <- SyncTimerEvent()
	ch <- Event{
		Event: []byte("SyncTracked"),
		Data:  []byte(renderTemplateToString("tracked", appState)),
	}
	ch <- Event{
		Event: []byte("SyncActivities"),
		Data:  []byte(renderTemplateToString("activities", appState)),
	}
	ch <- Event{
		Event: []byte("SyncMode"),
		Data:  []byte(renderTemplateToString("mode", appState)),
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
			// appState.CurrentPomo.Ticks++
			// TODO: REMOVE DEMO 1s=1m
			appState.CurrentPomo.Ticks += 60

			pomo := appState.CurrentPomo
			onBreak := pomo.IsBreak
			pomoComplete := time.Second*time.Duration(pomo.Ticks) >= pomo.Duration
			if !onBreak && !appState.FlowMode && appState.AutoBreak && pomoComplete {
				savePomo()
				startBreak(*pomo)
				go broadcastEvent(Event{
					Event: []byte("SyncControls"),
					Data:  []byte(renderTemplateToString("controls", appState)),
				})
			} else if !onBreak && pomoComplete && !pomo.FlowMode {
				// update controls to show start break btn
				go broadcastEvent(Event{
					Event: []byte("SyncControls"),
					Data:  []byte(renderTemplateToString("controls", appState)),
				})
			} else if onBreak && pomoComplete {
				appState.Running = false
				appState.CurrentPomo = nil
				go broadcastEvent(Event{
					Event: []byte("PomoReset"),
					Data:  []byte(renderTemplateToString("controls", appState)),
				})
				go broadcastEvent(SyncTimerEvent())
			}

			go broadcastEvent(SyncTimerEvent())
		case <-ping.C:
			broadcastEvent(Event{Event: []byte("Ping"), Data: []byte("keepalive")})

		}
	}
}

func startBreak(p Pomodoro) {
	// TODO:
	// [ ] improve logic, for now only works OK for 25m intervals
	// [ ] store breaks separately and calculate appropriate duration
	// 		according to totalPomosToday and stored breaks?

	appState.Running = true
	completedPomos := (time.Second * time.Duration(appState.TotalTicks())) / (25 * time.Minute)

	breakDuration := shortBreakDuration
	if p.FlowMode {
		breakDuration = (time.Duration(p.Ticks) * time.Second) / 5
	} else if completedPomos > 0 && completedPomos%4 == 0 {
		breakDuration = longBreakDuration
	}

	appState.CurrentPomo = &Pomodoro{
		StartedAt: time.Now().UTC(),
		Duration:  breakDuration,
		Ticks:     0,
		FlowMode:  false,
		Activity: Activity{
			Id:   0,
			Name: "Break",
		},
		IsBreak: true,
	}
}
