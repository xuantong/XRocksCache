/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

#include "commander.h"
#include "commands/command_parser.h"
#include "error_constants.h"
#include "server/redis_reply.h"
#include "server/server.h"
#include "storage/redis_db.h"
#include "time_util.h"
#include "ttl_util.h"
#include "types/redis_string.h"

namespace redis {

class CommandGet : public Commander {
 public:
  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    std::string value;
    redis::String string_db(srv->storage, conn->GetNamespace());
    auto s = string_db.Get(ctx, args_[1], &value);
    if (!s.ok() && !s.IsNotFound()) return {Status::RedisExecErr, s.ToString()};

    *output = s.IsNotFound() ? conn->NilString() : redis::BulkString(value);
    return Status::OK();
  }
};

class CommandMGet : public Commander {
 public:
  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    redis::String string_db(srv->storage, conn->GetNamespace());
    std::vector<Slice> keys;
    for (size_t i = 1; i < args_.size(); i++) {
      keys.emplace_back(args_[i]);
    }

    std::vector<std::string> values;
    auto statuses = string_db.MGet(ctx, keys, &values);
    *output = conn->MultiBulkString(values, statuses);
    return Status::OK();
  }
};

class CommandSet : public Commander {
 public:
  Status Parse(const std::vector<std::string> &args) override {
    CommandParser parser(args, 3);
    std::string_view ttl_flag, set_flag;
    while (parser.Good()) {
      if (auto v = GET_OR_RET(ParseExpireFlags(parser, ttl_flag))) {
        expire_ = *v;
      } else if (parser.EatEqICaseFlag("KEEPTTL", ttl_flag)) {
        keep_ttl_ = true;
      } else if (parser.EatEqICaseFlag("NX", set_flag)) {
        set_flag_ = StringSetType::NX;
      } else if (parser.EatEqICaseFlag("XX", set_flag)) {
        set_flag_ = StringSetType::XX;
      } else if (parser.EatEqICase("GET")) {
        get_ = true;
      } else {
        return parser.InvalidSyntax();
      }
    }

    return Status::OK();
  }

  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    std::optional<std::string> ret;
    redis::String string_db(srv->storage, conn->GetNamespace());
    auto s = string_db.Set(ctx, args_[1], args_[2], {expire_, set_flag_, get_, keep_ttl_, {}}, ret);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    if (get_) {
      *output = ret.has_value() ? redis::BulkString(ret.value()) : conn->NilString();
    } else {
      *output = ret.has_value() ? redis::RESP_OK : conn->NilString();
    }
    return Status::OK();
  }

 private:
  uint64_t expire_ = 0;
  bool get_ = false;
  bool keep_ttl_ = false;
  StringSetType set_flag_ = StringSetType::NONE;
};

class CommandMSet : public Commander {
 public:
  Status Parse(const std::vector<std::string> &args) override {
    if (args.size() % 2 != 1) return {Status::RedisParseErr, errWrongNumOfArguments};
    return Commander::Parse(args);
  }

  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    std::vector<StringPair> kvs;
    for (size_t i = 1; i < args_.size(); i += 2) {
      kvs.emplace_back(StringPair{args_[i], args_[i + 1]});
    }

    redis::String string_db(srv->storage, conn->GetNamespace());
    auto s = string_db.MSet(ctx, kvs);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    *output = redis::RESP_OK;
    return Status::OK();
  }
};

class CommandIncr : public Commander {
 public:
  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    int64_t ret = 0;
    redis::String string_db(srv->storage, conn->GetNamespace());
    auto s = string_db.IncrBy(ctx, args_[1], 1, &ret);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    *output = redis::Integer(ret);
    return Status::OK();
  }
};

class CommandDecr : public Commander {
 public:
  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    int64_t ret = 0;
    redis::String string_db(srv->storage, conn->GetNamespace());
    auto s = string_db.IncrBy(ctx, args_[1], -1, &ret);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    *output = redis::Integer(ret);
    return Status::OK();
  }
};

class CommandIncrBy : public Commander {
 public:
  Status Parse(const std::vector<std::string> &args) override {
    increment_ = GET_OR_RET(ParseInt<int64_t>(args[2], 10));
    return Commander::Parse(args);
  }

  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    int64_t ret = 0;
    redis::String string_db(srv->storage, conn->GetNamespace());
    auto s = string_db.IncrBy(ctx, args_[1], increment_, &ret);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    *output = redis::Integer(ret);
    return Status::OK();
  }

 private:
  int64_t increment_ = 0;
};

class CommandDecrBy : public Commander {
 public:
  Status Parse(const std::vector<std::string> &args) override {
    increment_ = GET_OR_RET(ParseInt<int64_t>(args[2], 10));
    if (increment_ == LLONG_MIN) return {Status::RedisParseErr, "decrement would overflow"};
    return Commander::Parse(args);
  }

  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    int64_t ret = 0;
    redis::String string_db(srv->storage, conn->GetNamespace());
    auto s = string_db.IncrBy(ctx, args_[1], -increment_, &ret);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    *output = redis::Integer(ret);
    return Status::OK();
  }

 private:
  int64_t increment_ = 0;
};

REDIS_REGISTER_COMMANDS(
    String, MakeCmdAttr<CommandGet>("get", 2, "read-only", 1, 1, 1),
    MakeCmdAttr<CommandMGet>("mget", -2, "read-only", 1, -1, 1),
    MakeCmdAttr<CommandSet>("set", -3, "write", 1, 1, 1),
    MakeCmdAttr<CommandMSet>("mset", -3, "write", 1, -1, 2),
    MakeCmdAttr<CommandIncrBy>("incrby", 3, "write", 1, 1, 1),
    MakeCmdAttr<CommandIncr>("incr", 2, "write", 1, 1, 1),
    MakeCmdAttr<CommandDecrBy>("decrby", 3, "write", 1, 1, 1),
    MakeCmdAttr<CommandDecr>("decr", 2, "write", 1, 1, 1), )

}  // namespace redis
