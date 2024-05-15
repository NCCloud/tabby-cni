package commmon

import "github.com/caarlos0/env/v11"

type Config struct {
	WatchKubevirtMigration bool   `env:"WATCH_KUBEVIRT_MIGRATION" envDefault:"false"`
	VirtualIPAddress       string `env:"VIRTUAL_IP_ADDRESS" envDefault:"169.254.1.1"`
}

func NewConfig() *Config {
	operatorConfig := &Config{}
	if err := env.Parse(operatorConfig); err != nil {
		panic(err)
	}

	return operatorConfig
}
