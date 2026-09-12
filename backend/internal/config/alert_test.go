package config

import "testing"

func TestAlertEvaluationSwitch(t *testing.T) {
	env := requiredEnvironment()
	cfg, e := LoadFrom(mapLookup(env))
	if e != nil || !cfg.AlertEvaluationEnabled {
		t.Fatal("default must enable evaluation")
	}
	env["ALERT_EVALUATION_ENABLED"] = "false"
	cfg, e = LoadFrom(mapLookup(env))
	if e != nil || cfg.AlertEvaluationEnabled {
		t.Fatal("explicit false ignored")
	}
	env["ALERT_EVALUATION_ENABLED"] = "invalid"
	if _, e = LoadFrom(mapLookup(env)); e == nil {
		t.Fatal("invalid boolean accepted")
	}
}
