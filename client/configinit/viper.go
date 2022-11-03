package configinit

import (
	"bytes"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
	"strings"
	"text/template"
)

func InitiateViper(v *viper.Viper, cmd *cobra.Command, environmentPrefix string) error {
	if err := bindAllFlags(v, cmd); err != nil {
		return err
	}

	bindEnvironment(v, environmentPrefix)

	return nil
}

func bindAllFlags(v *viper.Viper, cmd *cobra.Command) error {
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return err
	}
	if err := v.BindPFlags(cmd.PersistentFlags()); err != nil {
		return err
	}

	return nil
}

func bindEnvironment(v *viper.Viper, environmentPrefix string) {
	v.SetEnvPrefix(environmentPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
}

// WriteConfigToFile parses tmpl, renders config using the configTemplate and writes it to
// configFilePath.
func WriteConfigToFile(configFilePath string, configTemplate string, config any) error {
	var buffer bytes.Buffer

	tmpl := template.New(configFilePath)
	tmpl, err := tmpl.Parse(configTemplate)
	if err != nil {
		return err
	}

	if err := tmpl.Execute(&buffer, config); err != nil {
		return err
	}

	return os.WriteFile(configFilePath, buffer.Bytes(), 0o600)
}
