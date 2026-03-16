package output

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	"github.com/fatih/color"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

var (
	Green  = color.New(color.FgGreen, color.Bold).SprintFunc()
	Red    = color.New(color.FgRed, color.Bold).SprintFunc()
	Yellow = color.New(color.FgYellow).SprintFunc()
	Cyan   = color.New(color.FgCyan, color.Bold).SprintFunc()
)

func Success(msg string) {
	fmt.Println(Green("✓") + " " + msg)
}

func Error(msg string) {
	fmt.Fprintln(os.Stderr, Red("✗")+" "+msg)
}

func Info(msg string) {
	fmt.Println(Cyan("→") + " " + msg)
}

func StatusColor(status string) string {
	switch status {
	case "deployed", "running", "active":
		return Green(status)
	case "not_deployed", "stopped":
		return Yellow(status)
	case "failed", "error":
		return Red(status)
	default:
		return status
	}
}

func Table(headers []string, rows [][]string) {
	// Calculate actual column widths across headers and all rows
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			// Strip ANSI codes for width calculation
			plain := stripANSI(cell)
			if len(plain) > widths[i] {
				widths[i] = len(plain)
			}
		}
	}

	bold := color.New(color.Bold).SprintFunc()
	pad := func(s string, w int) string {
		plain := stripANSI(s)
		padding := w - len(plain)
		if padding < 0 {
			padding = 0
		}
		result := s
		for i := 0; i < padding; i++ {
			result += " "
		}
		return result
	}

	// Headers
	for i, h := range headers {
		if i > 0 {
			fmt.Print("   ")
		}
		fmt.Print(bold(pad(h, widths[i])))
	}
	fmt.Println()

	// Separator
	for i, w := range widths {
		if i > 0 {
			fmt.Print("   ")
		}
		for j := 0; j < w; j++ {
			fmt.Print("-")
		}
	}
	fmt.Println()

	// Rows
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Print("   ")
			}
			if i < len(widths)-1 {
				fmt.Print(pad(cell, widths[i]))
			} else {
				fmt.Print(cell)
			}
		}
		fmt.Println()
	}
}

func JSON(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func ExtractStatus(statusRaw json.RawMessage) string {
	if statusRaw == nil {
		return "unknown"
	}
	var s struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(statusRaw, &s); err == nil && s.Status != "" {
		return s.Status
	}
	return "unknown"
}
