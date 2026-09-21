package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DiskType                            string
	DiskPL                              string
	DiskCapacityGiB                     int64
	WriteRateMiB                        int
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
		DiskType: "cloud_essd", DiskPL: "pl1", DiskCapacityGiB: 100,
		WriteRateMiB:                        35,
		Bind:                                "127.0.0.1",
		Port:                                6666,
		Dir:                                 "data",
		LogDir:                              "stdout",
		LogLevel:                            "info",
		LogFormat:                           "text",
		LogRetentionDays:                    15,
		ActiveExpireEnabled:                 false,
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
		// 只支持整行注释，避免密码中的 # 被静默截断。
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return cfg, fmt.Errorf("%s:%d: invalid config line", path, lineNo)
		}
		key := strings.ToLower(fields[0])
		value := strings.Join(fields[1:], " ")

		switch key {
		case "disk-type":
			if value != "cloud_essd" {
				return cfg, fmt.Errorf("%s:%d: only cloud_essd is supported", path, lineNo)
			}
			cfg.DiskType = value
		case "disk-pl":
			if strings.ToLower(value) != "pl1" {
				return cfg, fmt.Errorf("%s:%d: only ESSD PL1 is supported", path, lineNo)
			}
			cfg.DiskPL = strings.ToLower(value)
		case "disk-capacity-gib":
			v, err := strconv.ParseInt(value, 10, 64)
			if err != nil || v < 20 {
				return cfg, fmt.Errorf("%s:%d: disk-capacity-gib must be at least 20", path, lineNo)
			}
			cfg.DiskCapacityGiB = v
		case "write-rate-mib":
			v, err := strconv.Atoi(value)
			if err != nil || v < 1 || v > 1024 {
				return cfg, fmt.Errorf("%s:%d: invalid write-rate-mib", path, lineNo)
			}
			cfg.WriteRateMiB = v
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
			// 拼写错误必须使启动失败，尤其不能静默忽略认证配置。
			return cfg, fmt.Errorf("%s:%d: unknown config key %q", path, lineNo, key)
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
