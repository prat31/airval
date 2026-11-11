package mock

import (
	"fmt"
	"regexp"
	"testing"
)

// The sample project's questions, one per template (see internal/api/handlers_sample.go).
var sampleLike = []string{
	"Why was I charged twice for order #12345?",
	"How do I update the card on file for account 23456?",
	"Can I get an invoice for order #34567 with my company VAT number?",
	"My Pulse earbuds subscription renewed early — can I get a prorated refund?",
	"Where is my order #45678? Tracking hasn't updated in three days.",
	"Can I change the delivery address for order #56789 after it shipped?",
	"Do you ship the Nimbus speaker to Norway, and how long does it take?",
	"Order #67890 arrived with a damaged box — what should I do?",
	"How do I return the Orbit smartwatch I bought last week?",
	"Is the return window for order #78901 30 or 60 days?",
	"Can I exchange my Aero headphones for a different color instead of a refund?",
	"I lost the receipt for order #89012 — can I still return it?",
	"How do I reset the password for account 90123?",
	"Can I merge my two accounts into account 11223?",
	"How do I turn on two-factor authentication for my Halo doorbell app?",
	"Please delete all personal data for account 33445.",
	"My Atlas router won't pair over Bluetooth after the latest update.",
	"Error E-55667 appears when I start the Comet tablet — how do I fix it?",
	"How do I factory-reset the Drift e-bike without losing my settings?",
	"Is firmware 77889 compatible with the Vector keyboard?",
	"What does the two-year warranty cover on the Lumen desk lamp?",
	"Is accidental damage to the Aero headphones covered?",
	"What are the setup steps for the Nimbus speaker?",
	"Which app do I need to configure the Orbit smartwatch?",
	"What is the battery life of the Pulse earbuds?",
	"Is the Halo doorbell water resistant?",
}

var sentence = regexp.MustCompile(`^[A-Z][^\n]{18,278}\.$`)

func TestAnswersAreNaturalAndDistinct(t *testing.T) {
	for _, q := range sampleLike {
		if ruleFor(q).template == fallbackRule.template {
			t.Errorf("%q fell through to the generic answer", q)
		}
	}
	for i := 0; i < 2000; i++ {
		q := fmt.Sprintf("%s (#%d)", sampleLike[i%len(sampleLike)], i)
		right, wrong := Truth(q), wrongAnswer(q)
		if right == wrong {
			t.Fatalf("wrong answer equals the truth for %q: %q", q, right)
		}
		if Truth(q) != right {
			t.Fatalf("Truth is not deterministic for %q", q)
		}
		for _, a := range []string{right, wrong} {
			if !sentence.MatchString(a) {
				t.Fatalf("answer %q does not match the sample's format rule", a)
			}
		}
	}
	if a := Truth("hi"); !sentence.MatchString(a) {
		t.Fatalf("fallback answer %q does not match the format rule", a)
	}
}
