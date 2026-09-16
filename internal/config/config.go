package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Bind                                string
	Port                                int
	Dir                                 string
	LogDir                              string
	LogLevel                            string
	LogFormat                           string
	LogRetentionDays                    int
	ActiveExpireEnabled                 bool
	ActiveExpireBucketSeconds           int
	ActiveExpireIntervalSeconds         int
	ActiveExpireCycleBudgetMilliseconds int
	ActiveExpireMaxDeletesPerCycle      int
	RequirePass                         string
	MaxClients                          int
	Workers                             int
	Profile                             bool
}

func Default() Config {
	return Config{
		Bind:                                "127.0.0.1",
		Port:                                6666,
		Dir:                                 "data",
		LogDir:                              "stdout",
		LogLevel:                            "info",
		LogFormat:                           "text",
		LogRetentionDays:                    15,
		ActiveExpireEnabled:                 true,
		ActiveExpireBucketSeconds:           30,
		ActiveExpireIntervalSeconds:         10,
		ActiveExpireCycleBudgetMilliseconds: 10,
		ActiveExpireMaxDeletesPerCycle:      1000,
		MaxClients:                          1024,
		Workers:                             2,
		Profile:                             true,
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return cfg, fmt.Errorf("%s:%d: invalid config line", path, lineNo)
		}
		key := strings.ToLower(fields[0])
		value := strings.Join(fields[1:], " ")

		switch key {
		case "bind":
			cfg.Bind = value
		case "port":
			v, err := strconv.Atoi(value)
			if err != nil || v <= 0 || v > 65535 {
				return cfg, fmt.Errorf("%s:%d: invalid port", path, lineNo)
			}
			cfg.Port = v
		case "dir":
			cfg.Dir = value
		case "log-dir":
			cfg.LogDir = value
		case "log-level":
			cfg.LogLevel = strings.ToLower(value)
		case "log-format":
			cfg.LogFormat = strings.ToLower(value)
		case "log-retention-days":
			v, err := strconv.Atoi(value)
			if err != nil || v < 0 {
				return cfg, fmt.Errorf("%s:%d: invalid log-retention-days", path, lineNo)
			}
			cfg.LogRetentionDays = v
		case "active-expire-enabled":
			v, err := parseBool(value)
			if err != nil {
				return cfg, fmt.Errorf("%s:%d: invalid active-expire-enabled", path, lineNo)
			}
			cfg.ActiveExpireEnabled = v
		case "active-expire-bucket-seconds":
			v, err := strconv.Atoi(value)
			if err != nil || v <= 0 || v > 3600 {
				return cfg, fmt.Errorf("%s:%d: invalid active-expire-bucket-seconds", path, lineNo)
			}
			cfg.ActiveExpireBucketSeconds = v
		case "active-expire-interval-seconds":
			v, err := strconv.Atoi(value)
			if err != nil || v <= 0 || v > 3600 {
				return cfg, fmt.Errorf("%s:%d: invalid active-expire-interval-seconds", path, lineNo)
			}
			cfg.ActiveExpireIntervalSeconds = v
		case "active-expire-cycle-budget-ms":
			v, err := strconv.Atoi(value)
			if err != nil || v <= 0 || v > 1000 {
				return cfg, fmt.Errorf("%s:%d: invalid active-expire-cycle-budget-ms", path, lineNo)
			}
			cfg.ActiveExpireCycleBudgetMilliseconds = v
		case "active-expire-max-deletes-per-cycle":
			v, err := strconv.Atoi(value)
			if err != nil || v <= 0 {
				return cfg, fmt.Errorf("%s:%d: invalid active-expire-max-deletes-per-cycle", path, lineNo)
			}
			cfg.ActiveExpireMaxDeletesPerCycle = v
		case "requirepass":
			cfg.RequirePass = value
		case "maxclients":
			v, err := strconv.Atoi(value)
			if err != nil || v <= 0 {
				return cfg, fmt.Errorf("%s:%d: invalid maxclients", path, lineNo)
			}
			cfg.MaxClients = v
		case "workers":
			v, err := strconv.Atoi(value)
			if err != nil || v <= 0 {
				return cfg, fmt.Errorf("%s:%d: invalid workers", path, lineNo)
			}
			cfg.Workers = v
		case "xrockscache-profile":
			cfg.Profile = strings.EqualFold(value, "yes") || strings.EqualFold(value, "true") || value == "1"
		default:
			// 未知配置键会被有意忽略。
			// 这样旧 profile 文件仍可读取，同时 Go 实现可以收敛历史存储细节配置面。
		}
	}
	if err := scanner.Err(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on":
		return true, nil
	case "no", "false", "0", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool")
	}
}
