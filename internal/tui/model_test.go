package tui

import (
"strings"
"testing"
"time"

tea "github.com/charmbracelet/bubbletea"
"github.com/schlubbi/query-tap/internal/aggregator"
)

// stubSource is a test double implementing DataSource with canned data.
type stubSource struct {
snapshot []aggregator.FingerprintStats
stats    aggregator.AggregatorStats
}

func (s *stubSource) Snapshot() []aggregator.FingerprintStats { return s.snapshot }
func (s *stubSource) Stats() aggregator.AggregatorStats      { return s.stats }

func newStubSource(stats []aggregator.FingerprintStats, agg aggregator.AggregatorStats) *stubSource {
return &stubSource{snapshot: stats, stats: agg}
}

func sampleStats() ([]aggregator.FingerprintStats, aggregator.AggregatorStats) {
fp := []aggregator.FingerprintStats{
{
Fingerprint: "SELECT * FROM users WHERE id = ?",
Count:       1234,
TotalNs:     12_345_000_000, // 12,345 ms
P50Ns:       10_000_000,     // 10 ms
P95Ns:       25_300_000,     // 25.3 ms
P99Ns:       98_100_000,     // 98.1 ms
QPS:         12.3,
Tags:        map[string]string{"app": "web"},
},
{
Fingerprint: "INSERT INTO events VALUES (?+)",
Count:       567,
TotalNs:     5_670_000_000,
P50Ns:       10_000_000,
P95Ns:       15_200_000,
P99Ns:       45_000_000,
QPS:         5.7,
Tags:        map[string]string{"app": "worker"},
},
{
Fingerprint: "SELECT * FROM orders WHERE user_id = ?",
Count:       234,
TotalNs:     4_680_000_000,
P50Ns:       20_000_000,
P95Ns:       50_100_000,
P99Ns:       120_000_000,
QPS:         2.3,
Tags:        map[string]string{"app": "api"},
},
}
agg := aggregator.AggregatorStats{
TotalEvents:        2035,
ActiveFingerprints: 3,
Evictions:          0,
}
return fp, agg
}

func TestNew_DefaultValues(t *testing.T) {
src := newStubSource(nil, aggregator.AggregatorStats{})
m := New(src, 25, 500*time.Millisecond)

if m.topN != 25 {
t.Errorf("expected topN=25, got %d", m.topN)
}
if m.interval != 500*time.Millisecond {
t.Errorf("expected interval=500ms, got %v", m.interval)
}
if m.sortCol != SortByTotalLatency {
t.Errorf("expected default sortCol=SortByTotalLatency(%d), got %d", SortByTotalLatency, m.sortCol)
}
if m.quitting {
t.Error("expected quitting=false")
}
if m.scrollOffset != 0 {
t.Errorf("expected scrollOffset=0, got %d", m.scrollOffset)
}
}

func TestUpdate_QuitOnQ(t *testing.T) {
src := newStubSource(nil, aggregator.AggregatorStats{})
m := New(src, 25, time.Second)

msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
updated, cmd := m.Update(msg)
um := updated.(Model)

if !um.quitting {
t.Error("expected quitting=true after pressing 'q'")
}
if cmd == nil {
t.Error("expected non-nil Cmd (tea.Quit) after pressing 'q'")
}
}

func TestUpdate_QuitOnCtrlC(t *testing.T) {
src := newStubSource(nil, aggregator.AggregatorStats{})
m := New(src, 25, time.Second)

msg := tea.KeyMsg{Type: tea.KeyCtrlC}
updated, cmd := m.Update(msg)
um := updated.(Model)

if !um.quitting {
t.Error("expected quitting=true after Ctrl-C")
}
if cmd == nil {
t.Error("expected non-nil Cmd (tea.Quit) after Ctrl-C")
}
}

func TestUpdate_SortColumnChange(t *testing.T) {
src := newStubSource(nil, aggregator.AggregatorStats{})
m := New(src, 25, time.Second)

tests := []struct {
key      rune
wantSort int
}{
{'1', SortByCount},
{'2', SortByTotalLatency},
{'3', SortByAvgLatency},
{'4', SortByP95},
{'5', SortByP99},
{'6', SortByQPS},
{'7', SortByFingerprint},
}

for _, tt := range tests {
msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tt.key}}
updated, _ := m.Update(msg)
um := updated.(Model)
if um.sortCol != tt.wantSort {
t.Errorf("key '%c': expected sortCol=%d, got %d", tt.key, tt.wantSort, um.sortCol)
}
}
}

func TestUpdate_ScrollUpDown(t *testing.T) {
fp, agg := sampleStats()
src := newStubSource(fp, agg)
m := New(src, 25, time.Second)
m.height = 10

// Scroll down.
msg := tea.KeyMsg{Type: tea.KeyDown}
updated, _ := m.Update(msg)
um := updated.(Model)
if um.scrollOffset != 1 {
t.Errorf("expected scrollOffset=1 after down, got %d", um.scrollOffset)
}

// Scroll up.
msg = tea.KeyMsg{Type: tea.KeyUp}
updated, _ = um.Update(msg)
um = updated.(Model)
if um.scrollOffset != 0 {
t.Errorf("expected scrollOffset=0 after up, got %d", um.scrollOffset)
}

// Scroll up past zero stays at 0.
msg = tea.KeyMsg{Type: tea.KeyUp}
updated, _ = um.Update(msg)
um = updated.(Model)
if um.scrollOffset != 0 {
t.Errorf("expected scrollOffset=0 (no negative), got %d", um.scrollOffset)
}
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
src := newStubSource(nil, aggregator.AggregatorStats{})
m := New(src, 25, time.Second)

msg := tea.WindowSizeMsg{Width: 120, Height: 40}
updated, _ := m.Update(msg)
um := updated.(Model)

if um.width != 120 {
t.Errorf("expected width=120, got %d", um.width)
}
if um.height != 40 {
t.Errorf("expected height=40, got %d", um.height)
}
}

func TestUpdate_TickRefreshesStats(t *testing.T) {
fp, agg := sampleStats()
src := newStubSource(fp, agg)
m := New(src, 25, time.Second)

msg := tickMsg(time.Now())
updated, cmd := m.Update(msg)
um := updated.(Model)

if len(um.stats) != 3 {
t.Errorf("expected 3 stats after tick, got %d", len(um.stats))
}
if cmd == nil {
t.Error("expected non-nil cmd (next tick) after tick")
}
}

func TestView_ContainsColumnHeaders(t *testing.T) {
fp, agg := sampleStats()
src := newStubSource(fp, agg)
m := New(src, 25, time.Second)
m.width = 120
m.height = 30
m.stats = fp
m.aggStats = agg

view := m.View()

expectedHeaders := []string{"Fingerprint", "Count", "Total(ms)", "Avg(ms)", "P95(ms)", "P99(ms)", "QPS"}
for _, h := range expectedHeaders {
if !strings.Contains(view, h) {
t.Errorf("expected view to contain header %q", h)
}
}
}

func TestView_ContainsStatusBar(t *testing.T) {
fp, agg := sampleStats()
src := newStubSource(fp, agg)
m := New(src, 25, time.Second)
m.width = 120
m.height = 30
m.stats = fp
m.aggStats = agg

view := m.View()

if !strings.Contains(view, "2,035") {
t.Errorf("expected view to contain total events '2,035', got:\n%s", view)
}
if !strings.Contains(view, "3 fingerprints") {
t.Errorf("expected view to contain '3 fingerprints', got:\n%s", view)
}
}

func TestView_ContainsQueryData(t *testing.T) {
fp, agg := sampleStats()
src := newStubSource(fp, agg)
m := New(src, 25, time.Second)
m.width = 120
m.height = 30
m.stats = fp
m.aggStats = agg

view := m.View()

if !strings.Contains(view, "SELECT * FROM users WHERE id = ?") {
t.Errorf("expected view to contain fingerprint text")
}
if !strings.Contains(view, "1,234") {
t.Errorf("expected view to contain count '1,234'")
}
if !strings.Contains(view, "app=web") {
t.Errorf("expected view to contain tag 'app=web'")
}
}

func TestView_EmptyStats_ShowsNoQueries(t *testing.T) {
src := newStubSource(nil, aggregator.AggregatorStats{})
m := New(src, 25, time.Second)
m.width = 80
m.height = 24

view := m.View()

if !strings.Contains(view, "No queries captured yet") {
t.Errorf("expected 'No queries captured yet' for empty stats, got:\n%s", view)
}
}

func TestView_QuittingShowsGoodbye(t *testing.T) {
src := newStubSource(nil, aggregator.AggregatorStats{})
m := New(src, 25, time.Second)
m.quitting = true

view := m.View()

if !strings.Contains(view, "Goodbye") {
t.Errorf("expected 'Goodbye' when quitting, got:\n%s", view)
}
}

func TestView_HelpBarPresent(t *testing.T) {
fp, agg := sampleStats()
src := newStubSource(fp, agg)
m := New(src, 25, time.Second)
m.width = 120
m.height = 30
m.stats = fp
m.aggStats = agg

view := m.View()

if !strings.Contains(view, "q to quit") {
t.Errorf("expected help bar with 'q to quit'")
}
if !strings.Contains(view, "1-7 to sort") {
t.Errorf("expected help bar with '1-7 to sort'")
}
}

func TestView_TopNLimitsRows(t *testing.T) {
fp, agg := sampleStats()
src := newStubSource(fp, agg)
m := New(src, 2, time.Second) // topN = 2, but 3 stats
m.width = 120
m.height = 30
m.stats = fp
m.aggStats = agg

view := m.View()

if !strings.Contains(view, "SELECT * FROM users WHERE id = ?") {
t.Error("expected first fingerprint to be visible")
}
if !strings.Contains(view, "INSERT INTO events VALUES") {
t.Error("expected second fingerprint to be visible")
}
if strings.Contains(view, "SELECT * FROM orders WHERE user_id = ?") {
t.Error("expected third fingerprint to be hidden (topN=2)")
}
}

func TestSortStats_ByCount(t *testing.T) {
stats := []aggregator.FingerprintStats{
{Fingerprint: "a", Count: 10},
{Fingerprint: "b", Count: 50},
{Fingerprint: "c", Count: 30},
}
sortStats(stats, SortByCount)
if stats[0].Fingerprint != "b" {
t.Errorf("expected 'b' first (count=50), got %q", stats[0].Fingerprint)
}
if stats[1].Fingerprint != "c" {
t.Errorf("expected 'c' second (count=30), got %q", stats[1].Fingerprint)
}
}

func TestSortStats_ByFingerprint(t *testing.T) {
stats := []aggregator.FingerprintStats{
{Fingerprint: "SELECT * FROM z"},
{Fingerprint: "INSERT INTO a"},
{Fingerprint: "DELETE FROM m"},
}
sortStats(stats, SortByFingerprint)
if stats[0].Fingerprint != "DELETE FROM m" {
t.Errorf("expected alphabetical first, got %q", stats[0].Fingerprint)
}
}

func TestSortStats_ByAvgLatency(t *testing.T) {
stats := []aggregator.FingerprintStats{
{Fingerprint: "a", TotalNs: 100, Count: 10}, // avg = 10
{Fingerprint: "b", TotalNs: 300, Count: 10}, // avg = 30
{Fingerprint: "c", TotalNs: 200, Count: 10}, // avg = 20
}
sortStats(stats, SortByAvgLatency)
if stats[0].Fingerprint != "b" {
t.Errorf("expected 'b' first (avg=30), got %q", stats[0].Fingerprint)
}
}
