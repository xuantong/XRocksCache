package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"xrockscache/internal/resp"
	"xrockscache/internal/store"
)

func (s *Server) execute(w *resp.Writer, args []string, authenticated *bool) bool {
	cmd := strings.ToUpper(args[0])
	switch cmd {
	case "PING":
		if len(args) > 1 {
			_ = w.BulkString(args[1])
		} else {
			_ = w.SimpleString("PONG")
		}
	case "ECHO":
		if len(args) != 2 {
			_ = w.Error("ERR wrong number of arguments for 'echo' command")
		} else {
			_ = w.BulkString(args[1])
		}
	case "AUTH":
		s.auth(w, args, authenticated)
	case "HELLO":
		if len(args) > 1 && args[1] != "2" {
			_ = w.Error("NOPROTO only RESP2 is supported")
			return false
		}
		if len(args) > 2 {
			_ = w.Error("ERR unsupported HELLO options; use AUTH first")
			return false
		}
		if !*authenticated {
			_ = w.Error("NOAUTH Authentication required.")
			return false
		}
		_ = w.ArrayLen(6)
		_ = w.BulkString("server")
		_ = w.BulkString("xrockscache")
		_ = w.BulkString("version")
		_ = w.BulkString(s.version)
		_ = w.BulkString("proto")
		_ = w.Integer(2)
	case "QUIT":
		_ = w.SimpleString("OK")
		return true
	case "GET":
		s.get(w, args)
	case "MGET":
		s.mget(w, args)
	case "SET":
		s.set(w, args)
	case "MSET":
		s.mset(w, args)
	case "DEL":
		s.del(w, args)
	case "EXISTS":
		s.exists(w, args)
	case "EXPIRE":
		s.expire(w, args, time.Second)
	case "PEXPIRE":
		s.expire(w, args, time.Millisecond)
	case "TTL":
		s.ttl(w, args, time.Second)
	case "PTTL":
		s.ttl(w, args, time.Millisecond)
	case "DBSIZE":
		_ = w.Integer(s.store.DBSize())
	case "INFO":
		s.info(w)
	case "SELECT":
		if len(args) == 2 && args[1] == "0" {
			_ = w.SimpleString("OK")
		} else {
			_ = w.Error("ERR XRocksCache only supports database 0")
		}
	case "COMMAND":
		_ = w.ArrayLen(0)
	case "CLIENT":
		s.client(w, args)
	case "INCR":
		s.incrBy(w, args, 1)
	case "DECR":
		s.incrBy(w, args, -1)
	case "INCRBY":
		s.incrByArg(w, args, 1)
	case "DECRBY":
		s.incrByArg(w, args, -1)
	default:
		_ = w.Error("ERR unknown command '" + args[0] + "'")
	}
	return false
}

func (s *Server) auth(w *resp.Writer, args []string, authenticated *bool) {
	if s.cfg.RequirePass == "" {
		*authenticated = true
		_ = w.SimpleString("OK")
		return
	}
	if len(args) != 2 && len(args) != 3 {
		_ = w.Error("ERR wrong number of arguments for 'auth' command")
		return
	}
	password := args[len(args)-1]
	*authenticated = false
	if password != s.cfg.RequirePass || (len(args) == 3 && args[1] != "default") {
		_ = w.Error("WRONGPASS invalid username-password pair or user is disabled.")
		return
	}
	*authenticated = true
	_ = w.SimpleString("OK")
}

func (s *Server) get(w *resp.Writer, args []string) {
	if len(args) != 2 {
		_ = w.Error("ERR wrong number of arguments for 'get' command")
		return
	}
	value, ok, err := s.store.GetWithError(args[1])
	if err != nil {
		_ = w.Error("ERR " + err.Error())
		return
	}
	if ok {
		_ = w.BulkBytes(value)
	} else {
		_ = w.Nil()
	}
}

func (s *Server) mget(w *resp.Writer, args []string) {
	if len(args) < 2 {
		_ = w.Error("ERR wrong number of arguments for 'mget' command")
		return
	}
	if len(args) > 65 {
		_ = w.Error("ERR MGET supports at most 64 keys")
		return
	}
	values, found, err := s.store.MGet(args[1:])
	if err != nil {
		_ = w.Error("ERR " + err.Error())
		return
	}
	_ = w.ArrayLen(len(args) - 1)
	for i, value := range values {
		if found[i] {
			_ = w.BulkBytes(value)
		} else {
			_ = w.Nil()
		}
	}
}

func (s *Server) set(w *resp.Writer, args []string) {
	if len(args) < 3 {
		_ = w.Error("ERR wrong number of arguments for 'set' command")
		return
	}
	opts := store.SetOptions{}
	seen := make(map[string]bool)
	for i := 3; i < len(args); i++ {
		option := strings.ToUpper(args[i])
		if seen[option] {
			_ = w.Error("ERR syntax error")
			return
		}
		seen[option] = true
		switch option {
		case "EX":
			i++
			if i >= len(args) {
				_ = w.Error("ERR syntax error")
				return
			}
			n, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || n <= 0 || n > int64(store.MaxTTL/time.Second) {
				_ = w.Error("ERR invalid expire time; ttl exceeds 15 days or is not positive")
				return
			}
			opts.TTL = time.Duration(n) * time.Second
		case "PX":
			i++
			if i >= len(args) {
				_ = w.Error("ERR syntax error")
				return
			}
			n, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || n <= 0 || n > int64(store.MaxTTL/time.Millisecond) {
				_ = w.Error("ERR invalid expire time; ttl exceeds 15 days or is not positive")
				return
			}
			opts.TTL = time.Duration(n) * time.Millisecond
		case "NX":
			opts.Mode = "NX"
		case "XX":
			opts.Mode = "XX"
		case "GET":
			opts.Get = true
		case "KEEPTTL":
			opts.KeepTTL = true
		default:
			_ = w.Error("ERR syntax error")
			return
		}
	}
	if (seen["NX"] && seen["XX"]) || (seen["EX"] && seen["PX"]) || (seen["KEEPTTL"] && (seen["EX"] || seen["PX"])) {
		_ = w.Error("ERR syntax error")
		return
	}
	old, stored, err := s.store.Set(args[1], []byte(args[2]), opts)
	if err != nil {
		_ = w.Error(err.Error())
		return
	}
	if opts.Get {
		if old == nil {
			_ = w.Nil()
		} else {
			_ = w.BulkBytes(old)
		}
		return
	}
	if !stored && opts.Mode != "" {
		_ = w.Nil()
		return
	}
	_ = w.SimpleString("OK")
}

func (s *Server) mset(w *resp.Writer, args []string) {
	if len(args) < 3 || len(args)%2 == 0 {
		_ = w.Error("ERR wrong number of arguments for 'mset' command")
		return
	}
	pairs := make(map[string][]byte, (len(args)-1)/2)
	for i := 1; i < len(args); i += 2 {
		pairs[args[i]] = []byte(args[i+1])
	}
	if err := s.store.MSet(pairs); err != nil {
		_ = w.Error(err.Error())
		return
	}
	_ = w.SimpleString("OK")
}

func (s *Server) del(w *resp.Writer, args []string) {
	if len(args) < 2 {
		_ = w.Error("ERR wrong number of arguments for 'del' command")
		return
	}
	n, err := s.store.Del(args[1:]...)
	if err != nil {
		_ = w.Error(err.Error())
		return
	}
	_ = w.Integer(n)
}

func (s *Server) exists(w *resp.Writer, args []string) {
	if len(args) < 2 {
		_ = w.Error("ERR wrong number of arguments for 'exists' command")
		return
	}
	n, err := s.store.ExistsWithError(args[1:]...)
	if err != nil {
		_ = w.Error("ERR " + err.Error())
		return
	}
	_ = w.Integer(n)
}

func (s *Server) expire(w *resp.Writer, args []string, unit time.Duration) {
	if len(args) != 3 {
		_ = w.Error("ERR wrong number of arguments for expire command")
		return
	}
	n, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || n > int64(store.MaxTTL/unit) {
		_ = w.Error("ERR invalid expire time")
		return
	}
	if n < 0 {
		n = 0
	}
	ok, err := s.store.Expire(args[1], time.Duration(n)*unit)
	if err != nil {
		_ = w.Error(err.Error())
		return
	}
	if ok {
		_ = w.Integer(1)
	} else {
		_ = w.Integer(0)
	}
}

func (s *Server) ttl(w *resp.Writer, args []string, unit time.Duration) {
	if len(args) != 2 {
		_ = w.Error("ERR wrong number of arguments for ttl command")
		return
	}
	ttl, exists, hasTTL, err := s.store.TTLWithError(args[1])
	if err != nil {
		_ = w.Error("ERR " + err.Error())
		return
	}
	if !exists {
		_ = w.Integer(-2)
		return
	}
	if !hasTTL {
		_ = w.Integer(-1)
		return
	}
	_ = w.Integer(int64(ttl / unit))
}

func (s *Server) info(w *resp.Writer) {
	stats := s.store.Stats()
	body := fmt.Sprintf(`# Server
xrockscache_version:%s
uptime_in_seconds:%d
connected_clients:%d

# Keyspace
db0:keys=%s,expires=%s

# XRocksCache
max_key_bytes:%s
max_value_bytes:%s
max_ttl_seconds:%s
aof_path:%s
rocksdb_path:%s
rocksdb_estimate_live_data_size:%s
rocksdb_pending_compaction_bytes:%s
rocksdb_auto_tuned:%s
rocksdb_compression:%s
rocksdb_disk_budget_bytes:%s
rocksdb_memory_budget_bytes:%s
rocksdb_block_cache_bytes:%s
rocksdb_write_buffer_bytes:%s
rocksdb_target_file_size_bytes:%s
rocksdb_blob_files_enabled:%s
rocksdb_min_blob_size_bytes:%s
rocksdb_blob_file_size_bytes:%s
rocksdb_blob_gc_enabled:%s
rocksdb_max_background_jobs:%s
rocksdb_max_subcompactions:%s
rocksdb_soft_pending_bytes:%s
rocksdb_hard_pending_bytes:%s
rocksdb_rate_limiter_bytes_sec:%s
rocksdb_periodic_compaction_sec:%s
disk_warn_watermark_bytes:%s
disk_slowdown_watermark_bytes:%s
disk_reject_watermark_bytes:%s
active_expire_enabled:%s
active_expire_bucket_seconds:%s
active_expire_interval_seconds:%s
active_expire_cycle_budget_ms:%s
active_expire_max_deletes_per_cycle:%s
`, s.version, int64(time.Since(s.started)/time.Second), s.active.Load(), stats["keys"], stats["expires"], stats["max_key"], stats["max_value"], stats["max_ttl_sec"], stats["aof_path"], stats["rocksdb_path"], stats["rocksdb_estimate_live_data_size"], stats["rocksdb_pending_compaction_bytes"], stats["rocksdb_auto_tuned"], stats["rocksdb_compression"], stats["rocksdb_disk_budget_bytes"], stats["rocksdb_memory_budget_bytes"], stats["rocksdb_block_cache_bytes"], stats["rocksdb_write_buffer_bytes"], stats["rocksdb_target_file_size_bytes"], stats["rocksdb_blob_files_enabled"], stats["rocksdb_min_blob_size_bytes"], stats["rocksdb_blob_file_size_bytes"], stats["rocksdb_blob_gc_enabled"], stats["rocksdb_max_background_jobs"], stats["rocksdb_max_subcompactions"], stats["rocksdb_soft_pending_bytes"], stats["rocksdb_hard_pending_bytes"], stats["rocksdb_rate_limiter_bytes_sec"], stats["rocksdb_periodic_compaction_sec"], stats["disk_warn_watermark_bytes"], stats["disk_slowdown_watermark_bytes"], stats["disk_reject_watermark_bytes"], stats["active_expire_enabled"], stats["active_expire_bucket_seconds"], stats["active_expire_interval_seconds"], stats["active_expire_cycle_budget_ms"], stats["active_expire_max_deletes_cycle"])
	for _, key := range []string{"disk_usage_bytes", "disk_free_bytes", "disk_reserve_bytes", "disk_sample_failed", "rocksdb_background_errors", "rocksdb_oldest_sst_age_sec", "rocksdb_immutable_memtables", "rocksdb_num_running_compactions", "rocksdb_num_running_flushes", "rocksdb_memtable_bytes", "rocksdb_l0_files"} {
		body += key + ":" + stats[key] + "\r\n"
	}
	body += "write_rate_bytes_sec:" + stats["write_rate_bytes_sec"] + "\r\n"
	_ = w.BulkString(body)
}

func (s *Server) client(w *resp.Writer, args []string) {
	if len(args) >= 2 {
		switch strings.ToUpper(args[1]) {
		case "SETINFO", "SETNAME":
			_ = w.SimpleString("OK")
			return
		case "GETNAME":
			_ = w.Nil()
			return
		case "ID":
			_ = w.Integer(0)
			return
		}
	}
	_ = w.Error("ERR unsupported CLIENT subcommand")
}

func (s *Server) incrBy(w *resp.Writer, args []string, delta int64) {
	if len(args) != 2 {
		_ = w.Error("ERR wrong number of arguments for increment command")
		return
	}
	v, err := s.store.IncrBy(args[1], delta)
	if err != nil {
		_ = w.Error(err.Error())
		return
	}
	_ = w.Integer(v)
}

func (s *Server) incrByArg(w *resp.Writer, args []string, sign int64) {
	if len(args) != 3 {
		_ = w.Error("ERR wrong number of arguments for increment command")
		return
	}
	delta, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || (sign == -1 && delta == math.MinInt64) {
		_ = w.Error("ERR value is not an integer or out of range")
		return
	}
	v, err := s.store.IncrBy(args[1], sign*delta)
	if err != nil {
		_ = w.Error(err.Error())
		return
	}
	_ = w.Integer(v)
}
