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
#include "error_constants.h"
#include "server/redis_reply.h"
#include "server/server.h"
#include "storage/redis_db.h"
#include "time_util.h"

namespace redis {

class CommandTTL : public Commander {
 public:
  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    redis::Database redis(srv->storage, conn->GetNamespace());
    int64_t ttl = 0;

    auto s = redis.TTL(ctx, args_[1], &ttl);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    *output = redis::Integer(ttl > 0 ? ttl / 1000 : ttl);
    return Status::OK();
  }
};

class CommandPTTL : public Commander {
 public:
  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    redis::Database redis(srv->storage, conn->GetNamespace());
    int64_t ttl = 0;

    auto s = redis.TTL(ctx, args_[1], &ttl);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    *output = redis::Integer(ttl);
    return Status::OK();
  }
};

class CommandExists : public Commander {
 public:
  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    std::vector<rocksdb::Slice> keys;
    keys.reserve(args_.size() - 1);
    for (size_t i = 1; i < args_.size(); i++) {
      keys.emplace_back(args_[i]);
    }

    uint32_t count = 0;
    redis::Database redis(srv->storage, conn->GetNamespace());
    auto s = redis.Exists(ctx, keys, &count);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    *output = redis::Integer(count);
    return Status::OK();
  }
};

class CommandExpire : public Commander {
 public:
  Status Parse(const std::vector<std::string> &args) override {
    ttl_ = GET_OR_RET(ParseInt<int64_t>(args[2], 10));
    return Status::OK();
  }

  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    redis::Database redis(srv->storage, conn->GetNamespace());
    auto s = redis.Expire(ctx, args_[1], ttl_ * 1000 + util::GetTimeStampMS());
    *output = redis::Integer(s.ok() ? 1 : 0);
    return Status::OK();
  }

 private:
  uint64_t ttl_ = 0;
};

class CommandPExpire : public Commander {
 public:
  Status Parse(const std::vector<std::string> &args) override {
    expire_at_ms_ = GET_OR_RET(ParseInt<int64_t>(args[2], 10)) + util::GetTimeStampMS();
    return Status::OK();
  }

  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    redis::Database redis(srv->storage, conn->GetNamespace());
    auto s = redis.Expire(ctx, args_[1], expire_at_ms_);
    *output = redis::Integer(s.ok() ? 1 : 0);
    return Status::OK();
  }

 private:
  uint64_t expire_at_ms_ = 0;
};

class CommandDel : public Commander {
 public:
  Status Execute(engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    std::vector<rocksdb::Slice> keys;
    keys.reserve(args_.size() - 1);
    for (size_t i = 1; i < args_.size(); i++) {
      keys.emplace_back(args_[i]);
    }

    uint64_t deleted = 0;
    redis::Database redis(srv->storage, conn->GetNamespace());
    auto s = redis.MDel(ctx, keys, &deleted);
    if (!s.ok()) return {Status::RedisExecErr, s.ToString()};

    *output = redis::Integer(deleted);
    return Status::OK();
  }
};

REDIS_REGISTER_COMMANDS(Key, MakeCmdAttr<CommandTTL>("ttl", 2, "read-only", 1, 1, 1),
                        MakeCmdAttr<CommandPTTL>("pttl", 2, "read-only", 1, 1, 1),
                        MakeCmdAttr<CommandExists>("exists", -2, "read-only", 1, -1, 1),
                        MakeCmdAttr<CommandExpire>("expire", 3, "write", 1, 1, 1),
                        MakeCmdAttr<CommandPExpire>("pexpire", 3, "write", 1, 1, 1),
                        MakeCmdAttr<CommandDel>("del", -2, "write no-dbsize-check", 1, -1, 1))

}  // namespace redis
