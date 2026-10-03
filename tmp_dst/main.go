package main

import (
	"fmt"
	"time"
)

func main() {
	loc, _ := time.LoadLocation("Pacific/Chatham")
	for _, mins := range []int{3*60 + 30, 3*60 + 44, 3*60 + 45, 3*60 + 46} {
		t := time.Date(2024, time.September, 29, mins/60, mins%60, 0, 0, loc)
		fmt.Printf("nominal %02d:%02d -> %s\n", mins/60, mins%60, t.Format("15:04:05 -0700 MST"))
	}
}
