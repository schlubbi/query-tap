// Package tui implements the interactive terminal dashboard for QueryTap.
package tui

import (
"fmt"
"sort"
"strings"
"time"

tea "github.com/charmbracelet/bubbletea"
"github.com/schlubbi/query-tap/internal/aggregator"
)

// Sort column constants.
const (
SortByCount        = iota // 0
SortByTotalLatency        // 1
SortByAvgLatency          // 2
SortByP95                 // 3
SortByP99                 // 4
SortByQPS                 // 5
SortByFingerprint         // 6
)

// version is displayed in the status bar.
const version = "v0.1.0-dev"

// DataSource abstracts the aggregator for testability.
type DataSource interface {
Snapshot() []aggregator.FingerprintStats
Stats() aggregator.AggregatorStats
}

// Model is the bubbletea model for the QueryTap TUI.
type Model struct {
source       DataSource
stats        []aggregator.FingerprintStats
aggStats     aggregator.AggregatorStats
width        int
height       int
sortCol      int           // which column to sort by
topN         int           // how many rows to display
interval     time.Duration // refresh interval
scrollOffset int
quitting     bool
}

// New creates a new TUI model.
func New(source DataSource, topN int, interval time.Duration) Model {
return Model{
source:   source,
topN:     topN,
interval: interval,
sortCol:  SortByTotalLatency,
}
}

// tickMsg triggers a periodic refresh.
type tickMsg time.Time

func tickCmd(d time.Duration) tea.Cmd {
return tea.Tick(d, func(t time.Time) tea.Msg {
return tickMsg(t)
})
}

// Init starts the first tick.
func (m Model) Init() tea.Cmd {
return tickCmd(m.interval)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
switch msg := msg.(type) {
case tea.WindowSizeMsg:
m.width = msg.Width
m.height = msg.Height
return m, nil

case tickMsg:
m.stats = m.source.Snapshot()
m.aggStats = m.source.Stats()
sortStats(m.stats, m.sortCol)
return m, tickCmd(m.interval)

case tea.KeyMsg:
return m.handleKey(msg)
}

return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
switch msg.Type {
case tea.KeyCtrlC:
m.quitting = true
return m, tea.Quit

case tea.KeyUp:
if m.scrollOffset > 0 {
m.scrollOffset--
}
return m, nil

case tea.KeyDown:
m.scrollOffset++
return m, nil

case tea.KeyRunes:
if len(msg.Runes) == 0 {
return m, nil
}
switch msg.Runes[0] {
case 'q':
m.quitting = true
return m, tea.Quit
case '1':
m.sortCol = SortByCount
sortStats(m.stats, m.sortCol)
case '2':
m.sortCol = SortByTotalLatency
sortStats(m.stats, m.sortCol)
case '3':
m.sortCol = SortByAvgLatency
sortStats(m.stats, m.sortCol)
case '4':
m.sortCol = SortByP95
sortStats(m.stats, m.sortCol)
case '5':
m.sortCol = SortByP99
sortStats(m.stats, m.sortCol)
case '6':
m.sortCol = SortByQPS
sortStats(m.stats, m.sortCol)
case '7':
m.sortCol = SortByFingerprint
sortStats(m.stats, m.sortCol)
}
}
return m, nil
}

// View renders the TUI.
func (m Model) View() string {
if m.quitting {
return "Goodbye!\n"
}

var b strings.Builder

// Status bar.
drops := m.aggStats.Evictions
var totalQPS float64
for _, s := range m.stats {
totalQPS += s.QPS
}
statusLine := fmt.Sprintf("QueryTap %s \u2014 %s events | %d fingerprints | %d drops | %.1f QPS",
version,
formatInt(m.aggStats.TotalEvents),
m.aggStats.ActiveFingerprints,
drops,
totalQPS,
)
b.WriteString(statusBarStyle.Render(statusLine))
b.WriteString("\n\n")

if len(m.stats) == 0 {
b.WriteString(emptyStyle.Render("No queries captured yet"))
b.WriteString("\n")
b.WriteString("\n")
b.WriteString(helpBarStyle.Render("Press q to quit \u2502 1-7 to sort \u2502 \u2191\u2193 to scroll"))
return b.String()
}

// Column widths.
const (
colRank  = 3
colFP    = 48
colCount = 8
colTotal = 11
colAvg   = 9
colP95   = 9
colP99   = 9
colQPS   = 7
colTags  = 20
)

// Header.
header := fmt.Sprintf(" %*s \u2502 %-*s \u2502 %*s \u2502 %*s \u2502 %*s \u2502 %*s \u2502 %*s \u2502 %*s \u2502 %s",
colRank, "#",
colFP, "Fingerprint",
colCount, "Count",
colTotal, "Total(ms)",
colAvg, "Avg(ms)",
colP95, "P95(ms)",
colP99, "P99(ms)",
colQPS, "QPS",
"Tags",
)
b.WriteString(headerStyle.Render(header))
b.WriteString("\n")

// Separator.
sep := fmt.Sprintf("\u2500%s\u2500\u253c\u2500%s\u2500\u253c\u2500%s\u2500\u253c\u2500%s\u2500\u253c\u2500%s\u2500\u253c\u2500%s\u2500\u253c\u2500%s\u2500\u253c\u2500%s\u2500\u253c\u2500%s",
strings.Repeat("\u2500", colRank),
strings.Repeat("\u2500", colFP),
strings.Repeat("\u2500", colCount),
strings.Repeat("\u2500", colTotal),
strings.Repeat("\u2500", colAvg),
strings.Repeat("\u2500", colP95),
strings.Repeat("\u2500", colP99),
strings.Repeat("\u2500", colQPS),
strings.Repeat("\u2500", colTags),
)
b.WriteString(separatorStyle.Render(sep))
b.WriteString("\n")

// Determine visible rows.
visible := m.stats
if m.topN > 0 && len(visible) > m.topN {
visible = visible[:m.topN]
}

// Apply scroll offset -- clamp to valid range.
maxScroll := len(visible)
offset := m.scrollOffset
if offset > maxScroll {
offset = maxScroll
}

// Calculate available data rows based on terminal height.
// Reserve: 2 (status+blank) + 1 (header) + 1 (separator) + 1 (blank) + 1 (help) = 6 lines.
availableRows := len(visible) - offset
if m.height > 0 {
dataArea := m.height - 6
if dataArea < 1 {
dataArea = 1
}
if availableRows > dataArea {
availableRows = dataArea
}
}
if availableRows < 0 {
availableRows = 0
}

for i := 0; i < availableRows; i++ {
idx := offset + i
if idx >= len(visible) {
break
}
s := visible[idx]

fp := s.Fingerprint
if len(fp) > colFP {
fp = fp[:colFP-1] + "\u2026"
}

totalMs := float64(s.TotalNs) / 1_000_000
avgMs := float64(0)
if s.Count > 0 {
avgMs = float64(s.TotalNs) / float64(s.Count) / 1_000_000
}
p95Ms := float64(s.P95Ns) / 1_000_000
p99Ms := float64(s.P99Ns) / 1_000_000

tagStr := formatTags(s.Tags)

row := fmt.Sprintf(" %*d \u2502 %-*s \u2502 %*s \u2502 %*s \u2502 %*s \u2502 %*s \u2502 %*s \u2502 %*s \u2502 %s",
colRank, idx+1,
colFP, fp,
colCount, formatInt(s.Count),
colTotal, formatFloat(totalMs),
colAvg, formatFloat(avgMs),
colP95, formatFloat(p95Ms),
colP99, formatFloat(p99Ms),
colQPS, formatFloat(s.QPS),
tagStr,
)

style := rowEvenStyle
if i%2 == 1 {
style = rowOddStyle
}
b.WriteString(style.Render(row))
b.WriteString("\n")
}

b.WriteString("\n")
b.WriteString(helpBarStyle.Render("Press q to quit \u2502 1-7 to sort \u2502 \u2191\u2193 to scroll"))

return b.String()
}

// sortStats sorts the slice in place by the given column.
func sortStats(stats []aggregator.FingerprintStats, col int) {
sort.SliceStable(stats, func(i, j int) bool {
switch col {
case SortByCount:
return stats[i].Count > stats[j].Count
case SortByTotalLatency:
return stats[i].TotalNs > stats[j].TotalNs
case SortByAvgLatency:
return avgNs(stats[i]) > avgNs(stats[j])
case SortByP95:
return stats[i].P95Ns > stats[j].P95Ns
case SortByP99:
return stats[i].P99Ns > stats[j].P99Ns
case SortByQPS:
return stats[i].QPS > stats[j].QPS
case SortByFingerprint:
return stats[i].Fingerprint < stats[j].Fingerprint
default:
return stats[i].TotalNs > stats[j].TotalNs
}
})
}

func avgNs(s aggregator.FingerprintStats) float64 {
if s.Count == 0 {
return 0
}
return float64(s.TotalNs) / float64(s.Count)
}

// formatInt formats a uint64 with comma separators (e.g., 1234 -> "1,234").
func formatInt[T uint64 | int](n T) string {
s := fmt.Sprintf("%d", n)
if len(s) <= 3 {
return s
}

var b strings.Builder
remainder := len(s) % 3
if remainder > 0 {
b.WriteString(s[:remainder])
}
for i := remainder; i < len(s); i += 3 {
if b.Len() > 0 {
b.WriteByte(',')
}
b.WriteString(s[i : i+3])
}
return b.String()
}

// formatFloat formats a float64 with one decimal place and comma separators.
func formatFloat(f float64) string {
if f < 0.05 {
return "0.0"
}
intPart := int(f)
frac := int((f - float64(intPart)) * 10)
return fmt.Sprintf("%s.%d", formatInt(intPart), frac)
}

// formatTags renders tags as "key=val,key=val" sorted by key.
func formatTags(tags map[string]string) string {
if len(tags) == 0 {
return ""
}
keys := make([]string, 0, len(tags))
for k := range tags {
keys = append(keys, k)
}
sort.Strings(keys)

parts := make([]string, 0, len(keys))
for _, k := range keys {
parts = append(parts, k+"="+tags[k])
}
return strings.Join(parts, ",")
}
