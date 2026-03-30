package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "ssg",
	Short: "A simple static site generator",
}

func init() {
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
		return
	}
}

// Not sure where to put this
func initConfig(cmd *cobra.Command, section string) (*viper.Viper, error) {
    inDir, _ := cmd.Flags().GetString("in")
    v := viper.New()
    v.SetConfigName("ssg")
    v.SetConfigType("toml")
    v.AddConfigPath(inDir)
    v.AddConfigPath(".")

    cmd.Flags().VisitAll(func(f *pflag.Flag) {
        v.BindPFlag(f.Name, f)
    })

    if err := v.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            return nil, fmt.Errorf("error reading config file: %w", err)
        }
    }

    if sub := v.Sub(section); sub != nil {
        for _, key := range sub.AllKeys() {
            v.SetDefault(key, sub.Get(key))
        }
    }

    return v, nil
}
