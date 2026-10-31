package rpc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/onboarding"
)

func TestOnboardingEndpointExposesWorkedMultiYearPlan(t *testing.T) {
	server := httptest.NewServer(NewHandler(&Server{}))
	defer server.Close()
	res, err := http.Get(server.URL + "/onboarding.json")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %s", res.Status)
	}
	var plan onboarding.Plan
	if err := json.NewDecoder(res.Body).Decode(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.TemplateID != onboarding.DefaultTemplateID || plan.ToMS <= plan.FromMS {
		t.Fatalf("plan = %#v", plan)
	}
	// The plan must carry the executable document, not just its name and
	// window: a client that opens the example needs a strategy to validate and
	// run, and fetching it separately is a second request that can fail alone.
	if len(plan.Strategy) == 0 {
		t.Fatal("plan strategy is empty; the first run has nothing to execute")
	}
	var strategy ir.Strategy
	if err := json.Unmarshal(plan.Strategy, &strategy); err != nil {
		t.Fatalf("plan strategy does not decode: %v", err)
	}
	if strategy.Template.ID != onboarding.DefaultTemplateID {
		t.Fatalf("plan strategy template = %q", strategy.Template.ID)
	}
	if len(strategy.Nodes) == 0 || !strategy.Entries.Long.Enabled {
		t.Fatalf("plan strategy is not runnable: %#v", strategy)
	}
}
