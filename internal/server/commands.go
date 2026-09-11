package server

import (
	"fmt"
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
		*authenticated = s.cfg.RequirePass == ""
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
	if password != s.cfg.RequirePass {
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
	if value, ok := s.store.Get(args[1]); ok {
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
	_ = w.ArrayLen(len(args) - 1)
	for _, key := range args[1:] {
		if value, ok := s.store.Get(key); ok {
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
	for i := 3; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "EX":
			i++
			if i >= len(args) {
				_ = w.Error("ERR syntax error")
				return
			}
			n, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || n <= 0 {
				_ = w.Error("ERR invalid expire time")
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
			if err != nil || n <= 0 {
				_ = w.Error("ERR invalid expire time")
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
	_ = w.Integer(s.store.Exists(args[1:]...))
}

func (s *Server) expire(w *resp.Writer, args []string, unit time.Duration) {
	if len(args) != 3 {
		_ = w.Error("ERR wrong number of arguments for expire command")
		return
	}
	n, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || n <= 0 {
		_ = w.Error("ERR invalid expire time")
		return
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
	ttl, exists, hasTTL := s.store.TTL(args[1])
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
db0:keys=%s,expires=unknown

# XRocksCache
max_key_bytes:%s
max_value_bytes:%s
max_ttl_seconds:%s
aof_path:%s
`, s.version, int64(time.Since(s.started)/time.Second), s.active.Load(), stats["keys"], stats["max_key"], stats["max_value"], stats["max_ttl_sec"], stats["aof_path"])
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
	if err != nil {
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
