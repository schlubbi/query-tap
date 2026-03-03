package tui

import "github.com/charmbracelet/lipgloss"

// Styles used throughout the TUI dashboard.
var (
	// statusBarStyle renders the top status line (dim).
	statusBarStyle = lipgloss.NewStyle().Faint(true)

	// headerStyle renders the column header row (bold).
	headerStyle = lipgloss.NewStyle().Bold(true)

	// separatorStyle renders the horizontal rule under headers.
	separatorStyle = lipgloss.NewStyle().Faint(true)

	// rowEvenStyle renders even-numbered data rows.
	rowEvenStyle = lipgloss.NewStyle()

	// rowOddStyle renders odd-numbered data rows with a subtle background.
	rowOddStyle = lipgloss.NewStyle().Faint(true)

	// helpBarStyle renders the bottom help line.
	helpBarStyle = lipgloss.NewStyle().Faint(true)

	// emptyStyle renders the "no data" placeholder.
	emptyStyle = lipgloss.NewStyle().Faint(true).Italic(true)
)
