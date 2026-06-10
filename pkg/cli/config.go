package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
)

// Sentinel constant for unset configuration values
const ConfigValueNotSet = "(not set)"

// ConfigEnvPrefix is the environment variable prefix for configuration keys.
const ConfigEnvPrefix = "PINNER_"

// configKeyToEnvVar converts a config key to its environment variable name.
// e.g., "base_endpoint" → "PINNER_BASE_ENDPOINT"
func configKeyToEnvVar(key string) string {
	return ConfigEnvPrefix + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
}

func newConfigCommand() *cli.Command {
	return &cli.Command{
		Name:     "config",
		Category: "System",
		Usage:    "View/set configuration",
		Description: `View or modify CLI configuration settings.

Usage:
  pinner config              Show all configuration values
  pinner config get <key>    Get specific configuration value
  pinner config set <key> <value>  Set configuration value

Examples:
  pinner config
  pinner config get base_endpoint
  pinner config set max_retries 5
  pinner config set secure false
  pinner config set memory_limit 256
  pinner config set max_retries 5 --dry-run

Configuration can also be set via environment variables with the PINNER_ prefix.
For example, PINNER_BASE_ENDPOINT sets base_endpoint, PINNER_SECURE sets secure.

Common keys:
  base_endpoint  - API endpoint (empty for default)
  secure         - Use HTTPS (true/false)
  max_retries    - Maximum retry attempts (default: 3)
  memory_limit   - Memory limit for CAR generation in MB (default: 100)
  auth_token     - Authentication token (managed by 'pinner auth')`,
		ArgsUsage: "[get <key> | set <key> <value>]",
			Flags: append(GlobalFlags(), DryRunFlag()),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			output := setupOutput(cmd)
			return configAction(ctx, cmd, output, defaultConfigManagerFactory)
		},
	}
}

func configAction(ctx context.Context, cmd *cli.Command, output Output, cfgMgrFactory ConfigManagerFactory) error {
	args := cmd.Args()

	if args.Len() == 0 {
		return showAllConfig(output, cfgMgrFactory)
	}

	action := args.Get(0)
	switch action {
	case "get":
		return getConfig(cmd, output, cfgMgrFactory)
	case "set":
		return setConfig(ctx, cmd, output, cfgMgrFactory)
	}

	return fmt.Errorf("invalid action: %s (use 'get' or 'set')", action)
}

func showAllConfig(output Output, cfgMgrFactory ConfigManagerFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	headers := []string{"Key", "Value", "Env", "Description"}
	rows := [][]string{}

	descriptions := cfgMgr.GetAllDescriptions()
	allConfig := cfgMgr.All()

	for key, description := range descriptions {
		value, exists := allConfig[key]
		var displayValue string

		if !exists {
			displayValue = ConfigValueNotSet
		} else {
			switch v := value.(type) {
			case string:
				displayValue = output.MaskSensitive(v, key)
			case bool:
				displayValue = strconv.FormatBool(v)
			case int, int64, float64:
				displayValue = fmt.Sprintf("%v", v)
			default:
				displayValue = fmt.Sprintf("%v", v)
			}

			if displayValue == "" {
				displayValue = ConfigValueNotSet
			}
		}

		if description == "" {
			description = "-"
		}

		envVar := configKeyToEnvVar(key)

		rows = append(rows, []string{key, displayValue, envVar, description})
	}

	output.PrintTable(headers, rows)
	return nil
}

func getConfig(cmd *cli.Command, output Output, cfgMgrFactory ConfigManagerFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	key := cmd.Args().Get(1)
	if key == "" {
		return fmt.Errorf("key is required for 'get' command. Run 'pinner config' to see all keys")
	}

	value, _, err := cfgMgr.Get(key)
	if err != nil {
		return fmt.Errorf("failed to get config key '%s': %w", key, err)
	}

	var displayValue string
	switch v := value.(type) {
	case string:
		displayValue = output.MaskSensitive(v, key)
	case bool:
		displayValue = strconv.FormatBool(v)
	case int, int64, float64:
		displayValue = fmt.Sprintf("%v", v)
	default:
		displayValue = fmt.Sprintf("%v", v)
	}

	if displayValue == "" {
		displayValue = "(not set)"
	}

	output.Print(displayValue)
	return nil
}

func setConfig(ctx context.Context, cmd *cli.Command, output Output, cfgMgrFactory ConfigManagerFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	key := cmd.Args().Get(1)
	valueStr := cmd.Args().Get(2)
	dryRun := cmd.Bool(FlagDryRun)

	if key == "" || valueStr == "" {
		return fmt.Errorf("key and value are required for 'set' command")
	}

	var currentValue any
	var currentExists bool
	if cfgMgr.Exists(key) {
		currentValue, _, _ = cfgMgr.Get(key)
		currentExists = true
	}

	var value any
	if cfgMgr.Exists(key) {
		switch currentValue.(type) {
		case bool:
			value, err = strconv.ParseBool(valueStr)
			if err != nil {
				return fmt.Errorf("invalid value for '%s': must be true or false", key)
			}
		case int, int64:
			value, err = strconv.ParseInt(valueStr, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid value for '%s': must be an integer", key)
			}
		case float64:
			value, err = strconv.ParseFloat(valueStr, 64)
			if err != nil {
				return fmt.Errorf("invalid value for '%s': must be a number", key)
			}
		default:
			value = valueStr
		}
	} else {
		value = valueStr
	}

	if dryRun {
		options := make(map[string]string)
		options[DryRunOptionKey] = key
		if currentExists {
			switch v := currentValue.(type) {
			case string:
				options[DryRunOptionCurrentValue] = output.MaskSensitive(v, key)
			default:
				options[DryRunOptionCurrentValue] = fmt.Sprintf("%v", v)
			}
		} else {
			options[DryRunOptionCurrentValue] = "(not set)"
		}
		switch v := value.(type) {
		case string:
			options[DryRunOptionNewValue] = output.MaskSensitive(v, key)
		default:
			options[DryRunOptionNewValue] = fmt.Sprintf("%v", v)
		}
		if desc := cfgMgr.GetDescription(key); desc != "" {
			options[DryRunOptionDescription] = desc
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "configuration change",
			Options:   options,
		})
		return nil
	}

	if err := cfgMgr.Set(ctx, key, value); err != nil {
		return fmt.Errorf("failed to set '%s': %w", key, err)
	}

	if err := cfgMgr.Persist(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	output.Printfln("Config updated: %s = %v", key, value)
	return nil
}
