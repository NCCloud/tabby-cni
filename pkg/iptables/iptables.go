package iptables

import (
	"fmt"
	"strings"

	"github.com/coreos/go-iptables/iptables"
	"github.com/sirupsen/logrus"
)

type Rules struct {
	table       string
	source      string
	destination string
	inface      string
	outface     string
	action      string
	chain       string
	comment     string
}

func renderRule(rule *Rules) []string {
	prepareRule := []string{}

	if rule.source != "" {
		prepareRule = append(prepareRule, "-s", rule.source)
	}

	if rule.destination != "" {
		prepareRule = append(prepareRule, "-d", rule.destination)
	}

	if rule.action != "" {
		prepareRule = append(prepareRule, "-j", rule.action)
	}

	if rule.comment != "" {
		prepareRule = append(prepareRule, "-m comment --comment", rule.comment)
	}

	return prepareRule
}

func AddRule(name string, source string, ignore []string) error {
	// Default iptables rules
	rules := []Rules{
		{
			table:  "nat",
			chain:  "POSTROUTING",
			source: source,
			action: fmt.Sprintf("POSTROUTING-%s", name),
		},
		{
			table:  "nat",
			chain:  fmt.Sprintf("POSTROUTING-%s", name),
			action: "MASQUERADE",
		},
		{
			table:       "nat",
			chain:       fmt.Sprintf("POSTROUTING-%s", name),
			destination: source,
			action:      "ACCEPT",
		},
	}

	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}

	ipt.NewChain("nat", fmt.Sprintf("POSTROUTING-%s", name))

	for _, r := range ignore {
		rules = append(
			rules, Rules{
				table:       "nat",
				chain:       fmt.Sprintf("POSTROUTING-%s", name),
				destination: r,
				action:      "ACCEPT",
			})
	}

	for _, rls := range rules {
		r := renderRule(&rls)

		exist, _ := ipt.Exists(rls.table, rls.chain, r...)
		if exist {
			logrus.WithFields(logrus.Fields{
				"chain": rls.chain,
				"table": rls.table,
				"rule":  strings.Join(r, " "),
			}).Info("Nothing to do, iptables rule already exists:")
		} else {
			logrus.WithFields(logrus.Fields{
				"chain": rls.chain,
				"table": rls.table,
				"rule":  strings.Join(r, " "),
			}).Info("iptables rule should be added:")

			err = ipt.Insert(rls.table, rls.chain, 1, r...)
			if err != nil {
				return fmt.Errorf("Failed to add iptables rule %v", err)
			}
		}
	}

	return nil
}

func PurgeChain(name string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		fmt.Println(err)
	}

	rules, err := ipt.List("nat", "POSTROUTING")
	if err != nil {
		return fmt.Errorf("Failed to get list of rules %v", err)
	}

	for _, rule := range rules {
		res := strings.Contains(rule, fmt.Sprintf("POSTROUTING-%s", name))
		if res == true {
			r := strings.Split(rule, " ")[2:]
			err = ipt.DeleteIfExists("nat", "POSTROUTING", r...)
			if err != nil {
				return fmt.Errorf("Failed to delete rule %s, %v", rule, err)
			}

			// Delete rules from postrouting
			err = ipt.ClearAndDeleteChain("nat", fmt.Sprintf("POSTROUTING-%s", name))
			if err != nil {
				return fmt.Errorf("Failed to delete rule %v", err)
			}
		}
	}

	return nil
}
