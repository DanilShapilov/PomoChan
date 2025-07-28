package main

import (
	"math/rand"
	"sort"
	"time"
)

var DemoPomodoros = []Pomodoro{}

type Stats struct {
	AppState        AppState
	Stats           []Pomodoro
	EfficiencyStats []EfficiencyStats
	DailyGoal       int
	DailyStats      []DailyStat
}
type EfficiencyStats struct {
	AveragePomodoros float64
	Hours            float64
	Efficiency       float64 // percent
	MissingPomodoros int
	DaysSelected     int
}

func CalculateEfficiency(days int, data []Pomodoro) EfficiencyStats {
	if days <= 0 {
		return EfficiencyStats{}
	}

	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -days)

	var totalPomodoros int
	var totalTicks int

	for _, p := range data {
		if p.IsBreak || p.FlowMode {
			continue
		}
		if p.StartedAt.After(startDate) && p.StartedAt.Before(endDate) {
			totalPomodoros++
			if p.Ticks > 0 {
				totalTicks += p.Ticks
			} else {
				// fallback to Duration if ticks not set
				totalTicks += int(p.Duration.Seconds())
			}
		}
	}

	average := float64(totalPomodoros) / float64(days)
	hours := float64(totalTicks) / 3600.0
	efficiency := (average / 8.0) * 100.0

	missing := 0
	if average < 8 {
		missing = int(8*float64(days) - float64(totalPomodoros))
	}

	return EfficiencyStats{
		AveragePomodoros: average,
		Hours:            hours,
		Efficiency:       efficiency,
		MissingPomodoros: missing,
		DaysSelected:     days,
	}
}

func getRandomActivity() Activity {
	keys := []int{}
	for k := range activities {
		keys = append(keys, k)
	}
	return activities[keys[rand.Intn(len(keys))]]
}

func generateDemoPomodoros() []Pomodoro {
	const pomodoroDuration = 25 * time.Minute
	now := time.Now().AddDate(0, 0, -1) //.Add(time.Hour * 12 * -1)
	startDate := now.AddDate(0, -6, 0)

	for day := 0; day <= int(now.Sub(startDate).Hours()/24); day++ {
		date := startDate.AddDate(0, 0, day)
		count := rand.Intn(15) // 0–20 pomodoros

		for i := 0; i < count; i++ {
			startTime := date.Add(time.Duration(rand.Intn(24*60)) * time.Minute)

			DemoPomodoros = append(DemoPomodoros, Pomodoro{
				StartedAt: startTime,
				Duration:  pomodoroDuration,
				Activity:  getRandomActivity(),
				FlowMode:  false,
				IsBreak:   false,
				Ticks:     int(pomodoroDuration.Seconds()),
			})
		}
	}
	return DemoPomodoros
}

type DailyStat struct {
	Date      string // "2025-07-27"
	Pomodoros int
}

func CalculateDailyStats(pomos []Pomodoro) []DailyStat {
	dayMap := make(map[string]int)
	for _, p := range pomos {
		day := p.StartedAt.Format("2006-01-02")
		dayMap[day] += 1
	}

	var stats []DailyStat
	for day, count := range dayMap {
		stats = append(stats, DailyStat{
			Date:      day,
			Pomodoros: count,
		})
	}

	// Sort by date
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Date < stats[j].Date
	})

	return stats
}
