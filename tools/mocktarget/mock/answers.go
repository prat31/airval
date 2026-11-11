package mock

import (
	"fmt"
	"strings"
)

// The mock answers like a real support assistant: an ordered set of keyword rules picks a
// topic-appropriate sentence, and the question's hash fixes one detail in it (a day count, version,
// duration...). The wrong answer is the same sentence with a different detail, so failures look
// realistic ("within 5 business days" vs "within 9") while staying exact-match checkable.

type answerRule struct {
	keywords []string // any match (lower-case substring) selects the rule
	template string   // exactly one %d
	lo, hi   int      // detail range, inclusive
}

var answerRules = []answerRule{
	// billing
	{[]string{"charged twice", "double charge"}, "I've reversed the duplicate charge; the refund reaches your card within %d business days.", 3, 10},
	{[]string{"card on file", "update the card", "payment method"}, "You can update your card under Billing → Payment methods; it's used from your next invoice in %d days.", 3, 28},
	{[]string{"invoice", "vat"}, "Your VAT invoice is available under Billing → Invoices, usually %d hours after payment.", 1, 48},
	{[]string{"prorated", "renewed early"}, "We've issued a prorated refund for the %d unused days of your plan to your original payment method.", 5, 28},
	// shipping
	{[]string{"tracking", "where is my order"}, "Your parcel is with the carrier and should arrive within %d business days.", 2, 9},
	{[]string{"delivery address"}, "The delivery address can be changed up to %d hours after dispatch through the carrier's tracking link.", 2, 24},
	{[]string{"damaged"}, "Sorry about that! Send us a photo of the damaged box within %d days and we'll ship a replacement.", 7, 30},
	{[]string{"ship", "deliver"}, "Yes, we ship there; standard delivery takes about %d business days.", 4, 12},
	// returns
	{[]string{"return window"}, "The return window is %d days from the delivery date.", 30, 60},
	{[]string{"receipt"}, "No receipt needed: we can process the return with your order number within %d days of purchase.", 30, 90},
	{[]string{"exchange"}, "Yes, you can exchange it for another color within %d days at no extra cost.", 14, 45},
	{[]string{"return"}, "Start a return from Orders → Return item; you have %d days from delivery.", 14, 60},
	// account
	{[]string{"password"}, "Use \"Forgot password\" on the sign-in page; the reset link stays valid for %d minutes.", 15, 60},
	{[]string{"merge"}, "Our support team can merge the accounts for you; it takes about %d business days.", 2, 7},
	{[]string{"two-factor", "2fa"}, "Turn it on under Settings → Security; you'll get %d one-time backup codes to keep safe.", 8, 12},
	{[]string{"personal data", "delete my account"}, "We'll delete your personal data within %d days and email you a confirmation.", 7, 30},
	// technical
	{[]string{"pair", "bluetooth"}, "Hold the power button for %d seconds to re-enter pairing mode, then reconnect from the app.", 5, 15},
	{[]string{"error"}, "That error is fixed in firmware 4.%d; update from the app under Device → Firmware.", 1, 9},
	{[]string{"factory-reset", "factory reset"}, "Back up your settings in the app, then hold both buttons for %d seconds to factory-reset.", 8, 20},
	{[]string{"firmware", "compatible"}, "Yes, firmware 4.%d and later are fully compatible.", 1, 9},
	// product specs (RAG dataset)
	{[]string{"accidental damage"}, "Accidental damage isn't covered by the warranty, but Acme Care covers it for %d months.", 12, 36},
	{[]string{"warranty"}, "The warranty covers manufacturing defects for %d months from the date of purchase.", 12, 24},
	{[]string{"setup steps", "set up"}, "Setup takes %d steps in the companion app: power on, pair, then name the device.", 3, 5},
	{[]string{"which app", "configure"}, "Use the Acme Home app, version 3.%d or later, to configure it.", 1, 9},
	{[]string{"battery"}, "The battery lasts up to %d hours on a full charge.", 8, 40},
	{[]string{"water"}, "It's rated IPX%d, so it handles splashes and rain.", 4, 7},
}

// fallbackRule answers anything the rules don't recognise.
var fallbackRule = answerRule{nil, "Thanks for asking! That's supported as of release 2.%d; please update and try again.", 1, 20}

func ruleFor(question string) answerRule {
	q := strings.ToLower(question)
	for _, r := range answerRules {
		for _, k := range r.keywords {
			if strings.Contains(q, k) {
				return r
			}
		}
	}
	return fallbackRule
}

func (r answerRule) detail(h uint64) int { return r.lo + int(h%uint64(r.hi-r.lo+1)) }

// wrongAnswer is the mock's incorrect answer: the right sentence with a different detail.
func wrongAnswer(question string) string {
	r := ruleFor(question)
	right := r.detail(hash64(question))
	d := r.detail(hash64("wrong:" + question))
	if d == right {
		d = r.lo + (right-r.lo+1)%(r.hi-r.lo+1)
	}
	return fmt.Sprintf(r.template, d)
}
