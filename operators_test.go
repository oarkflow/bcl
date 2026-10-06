package bcl

import (
	"testing"
	"time"
)

func TestOperatorsEvaluation(t *testing.T) {
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	vars := map[string]any{
		"message": map[string]any{
			"country":         "US",
			"provider_failed": "twilio_us_direct",
			"traffic_type":    "otp",
			"mno":             "Nepal Telecom (NTC)",
			"to":              "+1234567890",
			"customer_tier":   "vip",
			"active":          true,
			"inactive":        false,
			"empty_val":       "",
			"null_val":        nil,
			"tags":            []any{"sms", "urgent", "verified"},
			"allowed":         []any{"US", "CA", "GB"},
			"count":           42,
		},
		"date_str": "2024-06-15",
		"date_time": now,
	}

	tests := []struct {
		expr     string
		expected bool
	}{
		// Collection checks: IN
		{`message.country IN ("US", "CA")`, true},
		{`message.country IN ("GB", "DE")`, false},
		{`message.country IN ("US")`, true},
		{`message.country IN ()`, false},
		{`message.country IN ["US", "CA"]`, true},
		{`message.country IN ["GB", "DE"]`, false},
		{`message.country in ("US", "CA")`, true},
		{`message.country in ["US", "CA"]`, true},
		{`message.count IN (10, 42, 50)`, true},
		{`message.count IN [10, 20, 30]`, false},
		{`message.country IN message.allowed`, true},

		// Collection checks: NOT IN
		{`message.country NOT IN ("GB", "DE")`, true},
		{`message.country NOT IN ("US", "CA")`, false},
		{`message.country not in ("GB", "DE")`, true},
		{`message.country not in ["GB", "DE"]`, true},
		{`message.country not_in ["GB", "DE"]`, true},
		{`message.count NOT IN (1, 2, 3)`, true},

		// CONTAINS & NOT CONTAINS
		{`message.tags CONTAINS "urgent"`, true},
		{`message.tags CONTAINS "spam"`, false},
		{`message.tags NOT CONTAINS "spam"`, true},
		{`message.tags not contains "spam"`, true},
		{`message.tags not_contains "spam"`, true},

		// Range: BETWEEN
		{`message.count BETWEEN 10 AND 50`, true},
		{`message.count between 10 and 50`, true},
		{`message.count between 50 and 100`, false},
		{`date_str between '2024-01-01' AND '2024-12-31'`, true},
		{`date_str BETWEEN '2024-01-01' AND '2024-12-31'`, true},
		{`date_str between '2025-01-01' and '2025-12-31'`, false},
		{`date_time between '2024-01-01' AND '2024-12-31'`, true},
		{`date_time between '2025-01-01' and '2025-12-31'`, false},

		// Range: NOT BETWEEN
		{`message.count NOT BETWEEN 50 AND 100`, true},
		{`message.count not between 50 and 100`, true},
		{`message.count NOT BETWEEN 10 AND 50`, false},
		{`date_str NOT BETWEEN '2025-01-01' AND '2025-12-31'`, true},

		// Pattern matching: LIKE
		{`message.provider_failed LIKE "twilio_%"`, true},
		{`message.provider_failed LIKE "%_direct"`, true},
		{`message.provider_failed LIKE "%us%"`, true},
		{`message.provider_failed LIKE "carrier_%"`, false},
		{`message.provider_failed NOT LIKE "carrier_%"`, true},
		{`message.provider_failed not like "carrier_%"`, true},

		// Pattern matching: ILIKE
		{`message.provider_failed ILIKE "TWILIO_%"`, true},
		{`message.provider_failed ILIKE "%_DIRECT"`, true},
		{`message.provider_failed ilike "%US%"`, true},
		{`message.provider_failed NOT ILIKE "CARRIER_%"`, true},

		// Pattern matching: MATCHES
		{`message.mno MATCHES "^Nepal.*Telecom.*$"`, true},
		{`message.mno matches ".*NTC.*"`, true},
		{`message.mno NOT MATCHES "^[0-9]+$"`, true},
		{`message.mno not matches "^[0-9]+$"`, true},

		// STARTS_WITH & ENDS_WITH
		{`message.mno STARTS_WITH "Nepal"`, true},
		{`message.mno startswith "Nepal"`, true},
		{`message.mno NOT STARTS_WITH "India"`, true},
		{`message.mno ENDS_WITH "(NTC)"`, true},
		{`message.mno endswith "(NTC)"`, true},
		{`message.mno NOT ENDS_WITH "Airtel"`, true},

		// IS NULL & IS NOT NULL
		{`message.null_val IS NULL`, true},
		{`message.null_val is null`, true},
		{`message.country IS NOT NULL`, true},
		{`message.country is not null`, true},

		// IS EMPTY & IS NOT EMPTY
		{`message.empty_val IS EMPTY`, true},
		{`message.empty_val is empty`, true},
		{`message.country IS NOT EMPTY`, true},
		{`message.country is not empty`, true},

		// IS TRUE & IS FALSE
		{`message.active IS TRUE`, true},
		{`message.active is true`, true},
		{`message.inactive IS FALSE`, true},
		{`message.inactive is false`, true},
		{`message.active IS NOT FALSE`, true},
		{`message.inactive IS NOT TRUE`, true},

		// IS & IS NOT (equality)
		{`message.country IS "US"`, true},
		{`message.country is "US"`, true},
		{`message.country IS NOT "NP"`, true},
		{`message.country is not "NP"`, true},

		// Postfix EXISTS & EMPTY
		{`message.country EXISTS`, true},
		{`message.null_val NOT EXISTS`, true},
		{`message.empty_val EMPTY`, true},
		{`message.country NOT EMPTY`, true},

		// Synonyms
		{`message.count equals 42`, true},
		{`message.count not_equals 10`, true},
		{`message.count greater_than 40`, true},
		{`message.count less_than 50`, true},
		{`message.count greater_or_equal 42`, true},
		{`message.count less_or_equal 42`, true},

		// Complex chained logical expressions
		{`message.country IN ("US", "CA") AND message.traffic_type == "otp"`, true},
		{`message.country IN ("GB", "FR") OR message.traffic_type == "otp"`, true},
		{`message.country IN ("US", "CA") AND (message.count BETWEEN 10 AND 50)`, true},
		{`not (message.country == "NP")`, true},
		{`NOT (message.country == "NP")`, true},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			res, err := Eval(tt.expr, vars)
			if err != nil {
				t.Fatalf("unexpected error evaluating %q: %v", tt.expr, err)
			}
			b, ok := res.(bool)
			if !ok {
				t.Fatalf("expected bool result for %q, got %T (%v)", tt.expr, res, res)
			}
			if b != tt.expected {
				t.Errorf("evaluating %q: got %v, want %v", tt.expr, b, tt.expected)
			}
		})
	}
}

func TestBuiltinFunctions(t *testing.T) {
	vars := map[string]any{
		"text":  "carrier_ntc_smpp",
		"date":  "2024-06-15",
		"items": []any{"a", "b", "c"},
	}

	tests := []struct {
		expr     string
		expected bool
	}{
		{`like(text, "carrier_%")`, true},
		{`like(text, "%smpp")`, true},
		{`like(text, "other_%")`, false},
		{`ilike(text, "CARRIER_%")`, true},
		{`between(date, "2024-01-01", "2024-12-31")`, true},
		{`not_between(date, "2025-01-01", "2025-12-31")`, true},
		{`in("b", items)`, true},
		{`not_in("z", items)`, true},
		{`startswith(text, "carrier")`, true},
		{`endswith(text, "smpp")`, true},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			res, err := Eval(tt.expr, vars)
			if err != nil {
				t.Fatalf("unexpected error evaluating %q: %v", tt.expr, err)
			}
			b, ok := res.(bool)
			if !ok {
				t.Fatalf("expected bool result for %q, got %T (%v)", tt.expr, res, res)
			}
			if b != tt.expected {
				t.Errorf("evaluating %q: got %v, want %v", tt.expr, b, tt.expected)
			}
		})
	}
}

func TestSmsRoutingDecisionTableWithNewOperators(t *testing.T) {
	bclCode := `
bcl { version "1.0" }

module "sms_routing" {
  version "1.0"
  environment "production"

  decision_schema "provider_chain" { effects [allow, deny] default allow strategy first_match }
  decision_table "provider_chain" {
    default allow
    hit_policy first

    row "failover_us_twilio" {
      priority 980
      when {
        all {
          message.country IN ("US", "CA")
          message.provider_failed == "twilio_us_direct"
        }
      }
      then {
        outcome {
          decision allow
          attributes {
            provider "aws_sns_us"
            tier 2
          }
        }
      }
    }

    row "nepal_vip" {
      priority 890
      when {
        all {
          message.country == "NP"
          any {
            message.traffic_type IN ("otp", "critical_alert")
            message.customer_tier == "vip"
          }
        }
      }
      then {
        outcome {
          decision allow
          attributes {
            provider "carrier_ntc_smpp"
            tier 1
          }
        }
      }
    }

    row "failover_eu_messagebird" {
      priority 970
      when {
        all {
          message.country IN ("DE", "FR", "NL", "IT", "ES")
          message.provider_failed LIKE "%messagebird%"
        }
      }
      then {
        outcome {
          decision allow
          attributes {
            provider "sinch_eu_http"
            tier 2
          }
        }
      }
    }

    row "global_fallback" {
      priority 10
      when {
        true
      }
      then {
        outcome {
          decision allow
          attributes {
            provider "vonage_global_fallback"
            tier 3
          }
        }
      }
    }
  }
}
`

	doc, err := Parse([]byte(bclCode))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	prog, err := CompileDecisionDocument(doc, &Options{})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Test 1: US failover matching IN ("US", "CA")
	res1, err := EvaluateDecision(prog, "provider_chain", map[string]any{
		"message": map[string]any{
			"country":         "US",
			"provider_failed": "twilio_us_direct",
		},
	}, nil)
	if err != nil {
		t.Fatalf("evaluation error: %v", err)
	}
	if res1.Effect != "allow" {
		t.Errorf("expected effect 'allow', got %q", res1.Effect)
	}
	if res1.PolicyID != "failover_us_twilio" {
		t.Errorf("expected match 'failover_us_twilio', got %q", res1.PolicyID)
	}
	if p := res1.Attributes["provider"]; p != "aws_sns_us" {
		t.Errorf("expected provider 'aws_sns_us', got %v", p)
	}

	// Test 2: Nepal VIP matching any { traffic_type IN ("otp", "critical_alert") }
	res2, err := EvaluateDecision(prog, "provider_chain", map[string]any{
		"message": map[string]any{
			"country":      "NP",
			"traffic_type": "otp",
		},
	}, nil)
	if err != nil {
		t.Fatalf("evaluation error: %v", err)
	}
	if res2.PolicyID != "nepal_vip" {
		t.Errorf("expected match 'nepal_vip', got %q", res2.PolicyID)
	}
	if p := res2.Attributes["provider"]; p != "carrier_ntc_smpp" {
		t.Errorf("expected provider 'carrier_ntc_smpp', got %v", p)
	}

	// Test 3: EU failover matching LIKE "%messagebird%"
	res3, err := EvaluateDecision(prog, "provider_chain", map[string]any{
		"message": map[string]any{
			"country":         "DE",
			"provider_failed": "messagebird_eu",
		},
	}, nil)
	if err != nil {
		t.Fatalf("evaluation error: %v", err)
	}
	if res3.PolicyID != "failover_eu_messagebird" {
		t.Errorf("expected match 'failover_eu_messagebird', got %q", res3.PolicyID)
	}
	if p := res3.Attributes["provider"]; p != "sinch_eu_http" {
		t.Errorf("expected provider 'sinch_eu_http', got %v", p)
	}

	// Test 4: Catch-all rule matching `when { true }`
	res4, err := EvaluateDecision(prog, "provider_chain", map[string]any{
		"message": map[string]any{
			"country": "AU",
		},
	}, nil)
	if err != nil {
		t.Fatalf("evaluation error: %v", err)
	}
	if res4.PolicyID != "global_fallback" {
		t.Errorf("expected match 'global_fallback', got %q", res4.PolicyID)
	}
	if p := res4.Attributes["provider"]; p != "vonage_global_fallback" {
		t.Errorf("expected provider 'vonage_global_fallback', got %v", p)
	}
}

func TestOperatorsWithVariablesLeftAndRight(t *testing.T) {
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	vars := map[string]any{
		"target_country":   "NP",
		"allowed_countries": []any{"US", "CA", "NP", "IN"},
		"blocked_countries": []any{"KP", "IR", "SY"},
		"empty_list":        []any{},

		"user_roles":        []any{"admin", "editor", "operator"},
		"required_roles":    []any{"editor", "reviewer"},
		"required_all":      []any{"admin", "editor"},
		"forbidden_roles":   []any{"banned", "guest"},

		"score":      int64(85),
		"min_score":  int64(70),
		"max_score":  int64(100),
		"low_score":  int64(20),

		"tx_date":    "2024-06-15",
		"start_date": "2024-01-01",
		"end_date":   "2024-12-31",

		"tx_time":    now,
		"start_time": start,
		"end_time":   end,

		"provider":        "carrier_ntc_smpp",
		"pattern_like":    "%_smpp",
		"pattern_ilike":   "CARRIER_%",
		"bad_pattern":     "%_http",
		"regex_phone":     "^\\+977[0-9]+$",
		"phone":           "+9779841234567",
		"phone_prefix":    "+977",
		"bad_prefix":      "+1",
		"filename":        "report.json",
		"filename_suffix": ".json",
		"bad_suffix":      ".xml",

		"role":           "admin",
		"target_role":    "admin",
		"forbidden_role": "guest",

		"user": map[string]any{
			"id":      "usr-100",
			"country": "NP",
			"tier":    "vip",
		},
		"target": map[string]any{
			"id":      "usr-100",
			"country": "NP",
			"tier":    "vip",
		},
		"other": map[string]any{
			"id":      "usr-200",
			"country": "US",
			"tier":    "standard",
		},
	}

	tests := []struct {
		expr     string
		expected bool
	}{
		// 1. IN / NOT IN with variables
		{`target_country IN allowed_countries`, true},
		{`target_country IN blocked_countries`, false},
		{`"NP" IN allowed_countries`, true},
		{`"KP" IN allowed_countries`, false},
		{`target_country NOT IN blocked_countries`, true},
		{`target_country NOT IN allowed_countries`, false},
		{`"KP" NOT IN allowed_countries`, true},
		{`user.country IN allowed_countries`, true},
		{`user.country NOT IN blocked_countries`, true},

		// 2. CONTAINS / HAS with variables on Left and/or Right
		{`allowed_countries CONTAINS target_country`, true},
		{`blocked_countries CONTAINS target_country`, false},
		{`allowed_countries HAS target_country`, true},
		{`blocked_countries HAS target_country`, false},
		{`allowed_countries CONTAINS user.country`, true},
		{`blocked_countries NOT CONTAINS target_country`, true},
		{`blocked_countries not contains target_country`, true},
		{`blocked_countries NOT HAS target_country`, true},
		{`blocked_countries not has target_country`, true},

		// 3. HAS_ANY / HAS_ALL with variables
		{`user_roles HAS_ANY required_roles`, true},
		{`user_roles HAS_ANY forbidden_roles`, false},
		{`user_roles NOT HAS_ANY forbidden_roles`, true},
		{`user_roles not has_any forbidden_roles`, true},
		{`user_roles HAS_ALL required_all`, true},
		{`user_roles HAS_ALL required_roles`, false},
		{`user_roles NOT HAS_ALL forbidden_roles`, true},

		// 4. BETWEEN / NOT BETWEEN with variables on all positions
		{`score BETWEEN min_score AND max_score`, true},
		{`score between min_score and max_score`, true},
		{`low_score BETWEEN min_score AND max_score`, false},
		{`score NOT BETWEEN min_score AND max_score`, false},
		{`low_score NOT BETWEEN min_score AND max_score`, true},
		{`low_score not between min_score and max_score`, true},
		{`85 BETWEEN min_score AND max_score`, true},
		{`score BETWEEN 70 AND max_score`, true},
		{`score BETWEEN min_score AND 100`, true},
		{`min_score BETWEEN 50 AND max_score`, true},
		{`tx_date BETWEEN start_date AND end_date`, true},
		{`tx_date NOT BETWEEN "2025-01-01" AND end_date`, true},
		{`tx_time BETWEEN start_time AND end_time`, true},

		// 5. Pattern matching with variables on right
		{`provider LIKE pattern_like`, true},
		{`provider LIKE bad_pattern`, false},
		{`provider NOT LIKE bad_pattern`, true},
		{`"carrier_ntc_smpp" LIKE pattern_like`, true},
		{`provider ILIKE pattern_ilike`, true},
		{`provider NOT ILIKE bad_pattern`, true},
		{`phone MATCHES regex_phone`, true},
		{`phone NOT MATCHES "^[0-9]+$"`, true},

		// 6. Affixes with variables
		{`phone STARTS_WITH phone_prefix`, true},
		{`phone startswith phone_prefix`, true},
		{`phone NOT STARTS_WITH bad_prefix`, true},
		{`phone not starts_with bad_prefix`, true},
		{`filename ENDS_WITH filename_suffix`, true},
		{`filename endswith filename_suffix`, true},
		{`filename NOT ENDS_WITH bad_suffix`, true},
		{`filename not ends_with bad_suffix`, true},

		// 7. IS / IS NOT with variables
		{`role IS target_role`, true},
		{`role is target_role`, true},
		{`role IS NOT forbidden_role`, true},
		{`role is not forbidden_role`, true},
		{`user.id IS target.id`, true},
		{`user.id IS NOT other.id`, true},

		// 8. Relational synonyms with variables
		{`role equals target_role`, true},
		{`role not_equals forbidden_role`, true},
		{`score greater_than min_score`, true},
		{`low_score less_than min_score`, true},
		{`score greater_or_equal min_score`, true},
		{`score less_or_equal max_score`, true},
		{`min_score less_or_equal score`, true},

		// 9. Relational comparisons (left and right variables)
		{`min_score < score`, true},
		{`score > min_score`, true},
		{`min_score <= max_score`, true},
		{`max_score >= score`, true},
		{`user.id == target.id`, true},
		{`user.id != other.id`, true},
		{`user.tier == target.tier`, true},
		{`user.tier != other.tier`, true},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			res, err := EvalExpr(tt.expr, &EvalOptions{Variables: vars})
			if err != nil {
				t.Fatalf("eval error for %q: %v", tt.expr, err)
			}
			b, ok := res.(bool)
			if !ok {
				t.Fatalf("expected bool result for %q, got %T (%v)", tt.expr, res, res)
			}
			if b != tt.expected {
				t.Errorf("expected %q to be %v, got %v", tt.expr, tt.expected, b)
			}
		})
	}
}

func TestDecisionTableWithVariablesOnLeftAndRight(t *testing.T) {
	bclCode := `
module "dynamic_routing" {
  version "1.0"
  environment "production"

  decision_schema "route" { effects [allow, deny] default allow strategy first_match }
  decision_table "route" {
    default allow
    hit_policy first

    row "allowed_market_in_check" {
      priority 100
      when {
        all {
          message.country IN config.allowed_countries
          config.trusted_providers CONTAINS message.provider
          message.score BETWEEN config.min_score AND config.max_score
          message.provider LIKE config.provider_pattern
          message.role IS config.required_role
        }
      }
      then {
        outcome {
          decision allow
          attributes {
            route "fast_track"
            provider message.provider
          }
        }
      }
    }

    row "fallback" {
      priority 10
      when { true }
      then {
        outcome {
          decision allow
          attributes {
            route "standard"
            provider "default_provider"
          }
        }
      }
    }
  }
}
`
	doc, err := Parse([]byte(bclCode))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	prog, err := CompileDecisionDocument(doc, &Options{})
	if err != nil {
		t.Fatalf("compilation error: %v", err)
	}

	input := map[string]any{
		"message": map[string]any{
			"country":  "NP",
			"provider": "carrier_ntc_smpp",
			"score":    85,
			"role":     "admin",
		},
		"config": map[string]any{
			"allowed_countries": []any{"US", "CA", "NP"},
			"trusted_providers": []any{"carrier_ntc_smpp", "carrier_ncell_smpp"},
			"min_score":         70,
			"max_score":         100,
			"provider_pattern":  "%_smpp",
			"required_role":     "admin",
		},
	}

	res, err := EvaluateDecision(prog, "route", input, nil)
	if err != nil {
		t.Fatalf("evaluation error: %v", err)
	}
	if res.PolicyID != "allowed_market_in_check" {
		t.Fatalf("expected rule 'allowed_market_in_check' to match, got %q", res.PolicyID)
	}
	if res.Attributes["route"] != "fast_track" {
		t.Errorf("expected route 'fast_track', got %v", res.Attributes["route"])
	}

	// Now fail one variable check: score is below config.min_score
	inputFail := map[string]any{
		"message": map[string]any{
			"country":  "NP",
			"provider": "carrier_ntc_smpp",
			"score":    50, // below min_score 70
			"role":     "admin",
		},
		"config": map[string]any{
			"allowed_countries": []any{"US", "CA", "NP"},
			"trusted_providers": []any{"carrier_ntc_smpp"},
			"min_score":         70,
			"max_score":         100,
			"provider_pattern":  "%_smpp",
			"required_role":     "admin",
		},
	}

	resFail, err := EvaluateDecision(prog, "route", inputFail, nil)
	if err != nil {
		t.Fatalf("evaluation error: %v", err)
	}
	if resFail.PolicyID != "fallback" {
		t.Fatalf("expected fallback rule to match when score out of variable range, got %q", resFail.PolicyID)
	}
}

