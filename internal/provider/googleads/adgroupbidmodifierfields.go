package googleads

import (
	"slices"
	"time"
)

// The members of the AdGroupBidModifier resource of v26 that ad-group-bid-modifiers.create and .update offer.
// Not offered: the members Google fills itself (output only): criterion_id, base_ad_group, and
// bid_modifier_source; they are read with reports.search and an answer shows them. The resource has no
// deprecated member.

const (
	maxAdGroupBidModifiers = 10
	// minBidModifier and maxBidModifier bound bid_modifier: the reference allows 0.1 to 10.0, and 0 for a
	// device that is opted out.
	minBidModifier = 0.1
	maxBidModifier = 10.0
)

// adGroupBidModifierCriteria are the members of the criterion oneof of v26: exactly one names what the bid
// modifier applies to.
var adGroupBidModifierCriteria = []string{"device", "advance_booking_window", "length_of_booking",
	"start_date_range", "start_day_of_week", "user_set_dates"}

// checkBidModifierDate refuses a text that is no calendar date in the form YYYY-MM-DD.
func checkBidModifierDate(argument, value string) error {
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return badRequest(argument + " must be a calendar date in the form YYYY-MM-DD")
	}
	return nil
}

func bidModifierDateNode(name, desc string) node {
	n := textNode(name, desc, 10)
	n.pattern, n.check = datePattern.String(), checkBidModifierDate
	return n
}

func adGroupBidModifierMembers() []node {
	days := func(name, desc string) node { return wholeNode(name, desc, 0, math64) }
	return []node{
		createOnly(resourceNode("ad_group", "Ad group of this account the bid modifier belongs to, customers/ID/adGroups/N; fixed after creation", "adGroups")),
		{name: "bid_modifier", kind: kindNumber, bounded: true, low: 0, high: maxBidModifier,
			desc: "Factor for the bid of the ad group where the criterion applies, from 0.1 to 10.0; 0 opts out of a device and is accepted for the device criterion only"},
		createOnly(objectNode("device", "Criterion: a device type; fixed after creation. Exactly one criterion is required",
			enumNode("type", "Type of the device", enumOf("ad_group_bid_modifier.device.type")))),
		createOnly(objectNode("advance_booking_window", "Criterion: days between the booking and its start date; fixed after creation",
			days("min_days", "Low end of the number of days before the start date of the booking"),
			days("max_days", "High end of the number of days before the start date of the booking"))),
		createOnly(objectNode("length_of_booking", "Criterion: length of the booking in days; fixed after creation",
			days("min_days", "Low end of the number of days of the booking"),
			days("max_days", "High end of the number of days of the booking"))),
		createOnly(objectNode("start_date_range", "Criterion: range of the start date of the booking; fixed after creation",
			bidModifierDateNode("start_date", "Start of the range, YYYY-MM-DD"),
			bidModifierDateNode("end_date", "End of the range, YYYY-MM-DD"))),
		createOnly(objectNode("start_day_of_week", "Criterion: weekday of the start date of the booking; fixed after creation",
			enumNode("day_of_week", "The day of the week", enumOf("ad_group_bid_modifier.start_day_of_week.day_of_week")))),
		createOnly(objectNode("user_set_dates", "Criterion: default or user-selected dates; fixed after creation",
			enumNode("type", "Type of the date selection", enumOf("ad_group_bid_modifier.user_set_dates.type")))),
	}
}

// adGroupBidModifierTree holds the members of an ad group bid modifier that the tools offer.
var adGroupBidModifierTree = objectNode("ad_group_bid_modifier", "", adGroupBidModifierMembers()...).
	with(func(n *node) { n.exclusive = slices.Clone(adGroupBidModifierCriteria) })

// adGroupBidModifierOutputTree is the object Google returns with MUTABLE_RESOURCE: the members the tools
// offer, the members Google fills itself, and the resource name.
var adGroupBidModifierOutputTree = func() node {
	t := adGroupBidModifierTree
	t.fields = append(slices.Clone(t.fields),
		resourceNode("base_ad_group", "", "adGroups"),
		enumNode("bid_modifier_source", "", enumOf("ad_group_bid_modifier.bid_modifier_source")),
		wholeNode("criterion_id", "", 0, math64),
		textNode("resource_name", "", maxNameArgument))
	return t
}()
