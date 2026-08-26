package rpc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Bcypher01/Assay/internal/onboarding"
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
}
