package ebtables

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	ChainForward string = "FORWARD"
	cmdebtables  string = "ebtables-nft"
)

func checkIfRuleExists(listChainOutput string, args ...string) bool {
	rule := strings.Join(args, " ")
	for _, line := range strings.Split(listChainOutput, "\n") {
		if strings.TrimSpace(line) == rule {
			return true
		}
	}
	return false
}

func makeFullArgs(table string, op string, chain string, args ...string) []string {
	return append([]string{"-t", table, op, chain}, args...)
}

func AddRule(rule ...string) error {

	cmd := exec.Command(cmdebtables, "--list", ChainForward)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get list of ebtables rules: %v", err)
	}

	exist := checkIfRuleExists(string(stdout), rule...)

	if exist {
		logrus.WithFields(logrus.Fields{
			"chain": ChainForward,
			"table": "filter",
			"rule":  strings.Join(rule, " "),
		}).Info("Nothing to do, ebtables rule already exists:")
	} else {
		logrus.WithFields(logrus.Fields{
			"chain": ChainForward,
			"table": "filter",
			"rule":  strings.Join(rule, " "),
		}).Info("ebtables rule should be added:")

		fullargs := makeFullArgs("filter", "-I", ChainForward, rule...)
		cmd = exec.Command(cmdebtables, fullargs...)
		_, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to add ebtables rule %v, %v", rule, err)
		}
	}

	return nil
}

func DeleteRuleByDevice(bridge string) error {
	cmd := exec.Command(cmdebtables, "--list", ChainForward)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get list of ebtables rules: %v", err)
	}

	for _, line := range strings.Split(string(stdout), "\n") {
		if strings.Contains(line, bridge) {
			if err = DeleteRule(strings.TrimSpace(line)); err != nil {
				return fmt.Errorf(
					"failed to delete ebtables rule %q for bridge %q: %v", line, bridge, err)
			}
		}
	}

	return nil
}

func DeleteRule(rule string) error {
	r := strings.Split(rule, " ")
	fullargs := makeFullArgs("filter", "-D", ChainForward, r...)
	cmd := exec.Command(cmdebtables, fullargs...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}
