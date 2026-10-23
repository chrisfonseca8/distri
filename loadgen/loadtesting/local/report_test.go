package main

import (
	"math"
	"testing"
	"time"
)

func TestCalculateBaselineVarianceIgnoresSmoke(t *testing.T) {
	reports := []Report{
		{Label: "c1-smoke", Client: ClientMetrics{AcceptedRate: 999}},
		{Label: "c2-baseline-1", Client: ClientMetrics{AcceptedRate: 30, CompletedRate: 29, CheckoutLatency: LatencySummary{P95MS: 10}}},
		{Label: "c2-baseline-2", Client: ClientMetrics{AcceptedRate: 32, CompletedRate: 31, CheckoutLatency: LatencySummary{P95MS: 12}}},
	}

	result := calculateBaselineVariance(reports)
	if result == nil || result.Runs != 2 {
		t.Fatalf("expected two baseline runs, got %#v", result)
	}
	if result.AcceptedRate.Mean != 31 || result.CompletedRate.Mean != 30 {
		t.Fatalf("unexpected rate means: %#v", result)
	}
	if math.Abs(result.AcceptedRate.CoefficientPct-3.2258064516) > 0.0001 {
		t.Fatalf("unexpected accepted-rate CV: %f", result.AcceptedRate.CoefficientPct)
	}
}

func TestRunPassedRequiresExactCompletionAndCleanInvariants(t *testing.T) {
	cfg := config{CaseName: "C1", ExactOrders: 20}
	report := Report{
		Client: ClientMetrics{
			Offered: 20, Accepted: 20, Completed: 20,
			CheckoutStatusMix: newStatusMix(), PayStatusMix: newStatusMix(),
		},
		Invariants: []InvariantResult{{Name: "example", Passed: true}},
	}
	if !runPassed(report, cfg) {
		t.Fatal("expected a complete clean run to pass")
	}
	report.Client.Completed = 19
	if runPassed(report, cfg) {
		t.Fatal("expected an incomplete run to fail")
	}
}

func TestRunPassedAcceptsExactC5bSellout(t *testing.T) {
	cfg := config{CaseName: "C5", ExactOrders: 2000, HotStock: 1000}
	report := Report{
		Client: ClientMetrics{
			Offered: 2000, Accepted: 1000, Completed: 1000, Paid: 1000,
			CheckoutStatusMix: StatusMix{Accepted2xx: 1000, Business409: 1000},
			PayStatusMix:      StatusMix{Accepted2xx: 1000},
		},
		Invariants: []InvariantResult{{Name: "sellout", Passed: true}},
	}
	if !runPassed(report, cfg) {
		t.Fatal("expected exact C5b sellout to pass")
	}
}

func TestSweepDetection(t *testing.T) {
	reports := []Report{
		{Label: "c3-clients-01", Config: RunConfig{Clients: 1}, Client: ClientMetrics{CompletedRate: 3}},
		{Label: "c3-clients-02", Config: RunConfig{Clients: 2}, Client: ClientMetrics{CompletedRate: 6}},
		{Label: "c3-clients-05", Config: RunConfig{Clients: 5}, Client: ClientMetrics{CompletedRate: 6.4}},
		{Label: "c4-rate-010", Client: ClientMetrics{OfferedRate: 10, CompletedRate: 10}},
		{Label: "c4-rate-020", Client: ClientMetrics{OfferedRate: 20, CompletedRate: 18}},
	}
	clientPoints, knee := calculateClientSweep(reports)
	if len(clientPoints) != 3 || knee == nil || *knee != 2 {
		t.Fatalf("unexpected client sweep: points=%#v knee=%v", clientPoints, knee)
	}
	ratePoints, saturation := calculateRateSweep(reports)
	if len(ratePoints) != 2 || saturation == nil || *saturation != 20 {
		t.Fatalf("unexpected rate sweep: points=%#v saturation=%v", ratePoints, saturation)
	}
}

func TestExperimentMatrix(t *testing.T) {
	c3 := config{CaseName: "C3"}
	wantClients := []int{1, 2, 5, 10, 20, 50}
	for index, want := range wantClients {
		if got := c3.experiment(index + 1).Clients; got != want {
			t.Fatalf("C3 step %d: got %d clients, want %d", index+1, got, want)
		}
	}
	c4 := config{CaseName: "C4"}
	wantRates := []float64{10, 20, 40, 80}
	for index, want := range wantRates {
		if got := c4.experiment(index + 1).OfferedRate; got != want {
			t.Fatalf("C4 step %d: got %.0f/s, want %.0f/s", index+1, got, want)
		}
	}
	c5 := config{CaseName: "C5", Workload: "hot"}
	if first := c5.experiment(1); first.HotStock != fixtureStock || first.Duration != 60*time.Second {
		t.Fatalf("unexpected C5a config: %#v", first)
	}
	if second := c5.experiment(2); second.HotStock != 1000 || second.ExactOrders != 2000 {
		t.Fatalf("unexpected C5b config: %#v", second)
	}
}

func TestScenarioPointsSelectAndFlattenCases(t *testing.T) {
	reports := []Report{
		{Label: "c2-baseline-1", Config: RunConfig{Case: "C2"}},
		{
			Label:  "c13-gateway-0200ms",
			Config: RunConfig{Case: "C13", GatewayMode: "success", GatewayLatency: 200 * time.Millisecond},
			Client: ClientMetrics{CompletedRate: 30, PayLatency: LatencySummary{P50MS: 310}},
			Services: map[string]ServiceMetrics{
				"order": {DBPool: &DBPoolMetrics{WaitCount: 7, WaitDuration: 21 * time.Millisecond}},
			},
			Database: DatabaseMetrics{DeadlocksBefore: 1, DeadlocksAfter: 3},
		},
		{Label: "c13-gateway-0000ms", Config: RunConfig{Case: "C13", GatewayLatency: 0}},
		{Label: "c14-db-pool-05", Config: RunConfig{Case: "C14", DBPoolSize: 5}},
	}
	points := calculateScenarioPoints(reports, "C13")
	if len(points) != 2 || points[0].Label != "c13-gateway-0000ms" {
		t.Fatalf("expected the two C13 runs sorted by label, got %#v", points)
	}
	second := points[1]
	if second.GatewayLatencyMS != 200 || second.OrderPoolWaits != 7 || second.OrderPoolWaitMS != 21 ||
		second.PayP50MS != 310 || second.DeadlocksDelta != 2 || second.InventoryPoolWaits != 0 {
		t.Fatalf("unexpected flattened point: %#v", second)
	}
	if got := calculateScenarioPoints(reports, "C14"); len(got) != 1 || got[0].DBPoolSize != 5 {
		t.Fatalf("unexpected C14 selection: %#v", got)
	}
}

func TestRepeatHistoryKeepsEarlierRunsAndSpread(t *testing.T) {
	reports := []Report{
		{Label: "c2-baseline-1", Client: ClientMetrics{CompletedRate: 31}},
		{
			Label:        "c13-gateway-0000ms",
			Client:       ClientMetrics{CompletedRate: 300},
			PreviousRuns: []RunSummary{{CompletedRate: 200}, {CompletedRate: 250}},
		},
	}
	points := calculateRepeatHistory(reports)
	if len(points) != 1 || points[0].Label != "c13-gateway-0000ms" {
		t.Fatalf("expected only the repeated label, got %#v", points)
	}
	if got := points[0].Values; len(got) != 3 || got[0] != 200 || got[2] != 300 {
		t.Fatalf("expected oldest-first values 200, 250, 300, got %v", got)
	}
	if points[0].Stats.Mean != 250 || points[0].Stats.Min != 200 || points[0].Stats.Max != 300 {
		t.Fatalf("unexpected spread: %#v", points[0].Stats)
	}
}

func TestCheckDatabaseTargetRefusesUnlistedRemoteHosts(t *testing.T) {
	cases := []struct {
		name, url, allowed string
		wantErr            bool
	}{
		{"local compose database", "postgres://u:p@postgres:5432/distri", "", false},
		{"localhost", "postgres://u:p@localhost/distri", "", false},
		{"remote host without allow-list", "postgres://u:p@ep-test.neon.tech/neondb?sslmode=require", "", true},
		{"remote host matching allow-list", "postgres://u:p@ep-test.neon.tech/neondb", "ep-test.neon.tech", false},
		{"remote host not matching allow-list", "postgres://u:p@ep-other-pooler.neon.tech/neondb", "ep-test.neon.tech", true},
		{"garbage", "not a url", "", true},
	}
	for _, c := range cases {
		err := checkDatabaseTarget(c.url, c.allowed)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestCustomSweepSteps(t *testing.T) {
	c3 := config{CaseName: "C3", ClientSteps: []int{1, 100, 400}}
	if got := c3.experiment(3).Clients; got != 400 {
		t.Fatalf("C3 custom step 3: got %d clients, want 400", got)
	}
	c4 := config{CaseName: "C4", RateSteps: []float64{50, 500}}
	if got := c4.experiment(2).OfferedRate; got != 500 {
		t.Fatalf("C4 custom step 2: got %.0f/s, want 500", got)
	}
	t.Setenv("LOAD_CLIENT_STEPS", "1, 5,20")
	steps, err := envIntList("LOAD_CLIENT_STEPS")
	if err != nil || len(steps) != 3 || steps[2] != 20 {
		t.Fatalf("unexpected parsed steps %v (err %v)", steps, err)
	}
	t.Setenv("LOAD_CLIENT_STEPS", "1,zero")
	if _, err := envIntList("LOAD_CLIENT_STEPS"); err == nil {
		t.Fatal("expected an error for a non-numeric step")
	}
}

// C3 takes its client count from each sweep step, so the case itself has no client
// count; this must not be rejected as "load parameters must be positive".
func TestLoadConfigAcceptsC3SweepSteps(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@postgres:5432/distri")
	t.Setenv("LOAD_CASE", "C3")
	t.Setenv("LOAD_CLIENT_STEPS", "1,50,200")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("C3 with custom steps must be accepted: %v", err)
	}
	if cfg.Runs != 3 || cfg.experiment(3).Clients != 200 {
		t.Fatalf("expected 3 runs ending at 200 clients, got runs=%d clients=%d", cfg.Runs, cfg.experiment(3).Clients)
	}
	t.Setenv("LOAD_CLIENT_STEPS", "")
	cfg, err = loadConfig()
	if err != nil || cfg.Runs != 6 || cfg.experiment(6).Clients != 50 {
		t.Fatalf("default C3 sweep must still be 6 steps ending at 50 clients: runs=%d err=%v", cfg.Runs, err)
	}
}

func TestClientOverrideAndConnectionCap(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@postgres:5432/distri")
	t.Setenv("LOAD_CASE", "C9")
	t.Setenv("LOAD_CLIENTS", "150")
	t.Setenv("LOAD_MAX_CONNS", "1500")
	cfg, err := loadConfig()
	if err != nil || cfg.Clients != 150 || cfg.MaxConns != 1500 {
		t.Fatalf("C9 should take 150 clients and a 1500 connection cap: %+v err=%v", cfg, err)
	}
	t.Setenv("LOAD_MAX_CONNS", "")
	t.Setenv("LOAD_CLIENTS", "")
	cfg, err = loadConfig()
	if err != nil || cfg.Clients != 50 || cfg.MaxConns != 200 {
		t.Fatalf("defaults must stay 50 clients and a 200 connection cap: %+v err=%v", cfg, err)
	}
	t.Setenv("LOAD_MAX_CONNS", "0")
	if _, err := loadConfig(); err == nil {
		t.Fatal("a zero connection cap must be rejected")
	}
}

func TestParseCPUStatUsage(t *testing.T) {
	content := "usage_usec 123456\nuser_usec 100000\nsystem_usec 23456\nnr_periods 0\n"
	if value, ok := parseCPUStatUsage(content); !ok || value != 123456 {
		t.Fatalf("expected 123456, got %d (ok=%v)", value, ok)
	}
	if _, ok := parseCPUStatUsage("user_usec 5\n"); ok {
		t.Fatal("missing usage_usec must not parse")
	}
}
