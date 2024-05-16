package commmon

import "github.com/caarlos0/env/v11"

type Config struct {
	WatchKubevirtMigration bool `env:"WATCH_KUBEVIRT_MIGRATION" envDefault:"false"`
}

func NewConfig() *Config {
	operatorConfig := &Config{}
	if err := env.Parse(operatorConfig); err != nil {
		panic(err)
	}

	return operatorConfig
}
