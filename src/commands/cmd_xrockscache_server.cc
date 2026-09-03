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

#include <optional>

#include "commander.h"
#include "common/string_util.h"
#include "common/time_util.h"
#include "config/config.h"
#include "error_constants.h"
#include "server/redis_connection.h"
#include "server/redis_reply.h"
#include "server/server.h"

namespace redis {

class CommandAuth : public Commander {
 public:
  Status Execute([[maybe_unused]] engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    const auto &user_password = args_[1];
    std::string ns;
    AuthResult result = srv->AuthenticateUser(user_password, &ns);
    switch (result) {
      case AuthResult::NO_REQUIRE_PASS:
        return {Status::RedisExecErr, "Client sent AUTH, but no password is set"};
      case AuthResult::INVALID_PASSWORD:
        return {Status::RedisExecErr, "Invalid password"};
      case AuthResult::IS_USER:
        conn->BecomeUser();
        break;
      case AuthResult::IS_ADMIN:
        conn->BecomeAdmin();
        break;
    }
    conn->SetNamespace(ns);
    *output = redis::RESP_OK;
    return Status::OK();
  }
};

class CommandPing : public Commander {
 public:
  Status Execute([[maybe_unused]] engine::Context &ctx, [[maybe_unused]] Server *srv, [[maybe_unused]] Connection *conn,
                 std::string *output) override {
    if (args_.size() == 1) {
      *output = redis::SimpleString("PONG");
    } else if (args_.size() == 2) {
      *output = redis::BulkString(args_[1]);
    } else {
      return {Status::NotOK, errWrongNumOfArguments};
    }
    return Status::OK();
  }
};

class CommandInfo : public Commander {
 public:
  Status Parse(const std::vector<std::string> &args) override {
    for (size_t i = 1; i < args.size(); ++i) {
      if (util::EqualICase(args[i], "format")) {
        if (i + 1 >= args.size()) return {Status::RedisParseErr, errInvalidSyntax};
        const auto &fmt = args[++i];
        if (util::EqualICase(fmt, "json")) {
          format_ = Server::InfoFormat::Json;
        } else if (util::EqualICase(fmt, "txt")) {
          format_ = Server::InfoFormat::Text;
        } else {
          return {Status::RedisParseErr, errInvalidSyntax};
        }
      } else {
        sections_.push_back(args[i]);
      }
    }
    return Status::OK();
  }

  Status Execute([[maybe_unused]] engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    auto info = srv->GetInfo(conn->GetNamespace(), sections_, format_);
    *output = conn->VerbatimString("txt", info);
    return Status::OK();
  }

 private:
  std::vector<std::string> sections_;
  Server::InfoFormat format_ = Server::InfoFormat::Text;
};

class CommandDBSize : public Commander {
 public:
  Status Execute([[maybe_unused]] engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    if (args_.size() == 1) {
      KeyNumStats stats;
      srv->GetLatestKeyNumStats(conn->GetNamespace(), &stats);
      *output = redis::Integer(stats.n_key);
      return Status::OK();
    }
    if (args_.size() == 2 && util::EqualICase(args_[1], "scan")) {
      return srv->AsyncScanDBSize(conn->GetNamespace());
    }
    return {Status::RedisExecErr, "DBSIZE subcommand only supports scan"};
  }
};

class CommandClient : public Commander {
 public:
  Status Parse(const std::vector<std::string> &args) override {
    subcommand_ = util::ToLower(args[1]);
    if ((subcommand_ == "id" || subcommand_ == "getname" || subcommand_ == "list" || subcommand_ == "info") &&
        args.size() == 2) {
      return Status::OK();
    }
    if (subcommand_ == "setname" && args.size() == 3) {
      for (auto ch : args[2]) {
        if (ch < '!' || ch > '~') {
          return {Status::RedisInvalidCmd, "Client names cannot contain spaces, newlines or special characters"};
        }
      }
      conn_name_ = args[2];
      return Status::OK();
    }
    if (subcommand_ == "setinfo") {
      if (args.size() != 4) return {Status::RedisParseErr, errInvalidSyntax};
      auto attr = util::ToLower(args[2]);
      for (auto ch : args[3]) {
        if (ch < '!' || ch > '~') {
          return {Status::RedisInvalidCmd,
                  "lib-name and lib-ver cannot contain spaces, newlines or special characters"};
        }
      }
      if (attr == "lib-name") {
        setinfo_lib_name_ = args[3];
      } else if (attr == "lib-ver") {
        setinfo_lib_ver_ = args[3];
      } else {
        return {Status::RedisInvalidCmd, "Unrecognized option '" + args[2] + "'"};
      }
      return Status::OK();
    }
    if (subcommand_ == "reply") {
      if (args.size() != 3) return {Status::RedisParseErr, errInvalidSyntax};
      auto mode = util::ToLower(args[2]);
      if (mode == "on") {
        reply_mode_ = redis::Connection::ReplyMode::ON;
      } else if (mode == "off") {
        reply_mode_ = redis::Connection::ReplyMode::OFF;
      } else if (mode == "skip") {
        reply_mode_ = redis::Connection::ReplyMode::SKIP;
      } else {
        return {Status::RedisParseErr, errInvalidSyntax};
      }
      return Status::OK();
    }
    return {Status::RedisInvalidCmd, "Syntax error, try CLIENT LIST|INFO|GETNAME|SETNAME|SETINFO|REPLY|ID"};
  }

  Status Execute([[maybe_unused]] engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    if (subcommand_ == "list") {
      *output = conn->VerbatimString("txt", srv->GetClientsStr(conn));
    } else if (subcommand_ == "info") {
      *output = conn->VerbatimString("txt", conn->ToString());
    } else if (subcommand_ == "setname") {
      conn->SetName(conn_name_);
      *output = redis::RESP_OK;
    } else if (subcommand_ == "getname") {
      auto name = conn->GetName();
      *output = name.empty() ? conn->NilString() : redis::BulkString(name);
    } else if (subcommand_ == "id") {
      *output = redis::Integer(conn->GetID());
    } else if (subcommand_ == "reply") {
      conn->SetReplyMode(reply_mode_);
      if (reply_mode_ == redis::Connection::ReplyMode::SKIP) return Status::OK();
      *output = redis::RESP_OK;
    } else if (subcommand_ == "setinfo") {
      if (setinfo_lib_name_) conn->SetLibName(*setinfo_lib_name_);
      if (setinfo_lib_ver_) conn->SetLibVer(*setinfo_lib_ver_);
      *output = redis::RESP_OK;
    } else {
      return {Status::RedisInvalidCmd, "Syntax error, try CLIENT LIST|INFO|GETNAME|SETNAME|SETINFO|REPLY|ID"};
    }
    return Status::OK();
  }

 private:
  std::string conn_name_;
  std::string subcommand_;
  std::optional<std::string> setinfo_lib_name_;
  std::optional<std::string> setinfo_lib_ver_;
  redis::Connection::ReplyMode reply_mode_ = redis::Connection::ReplyMode::ON;
};

class CommandCommand : public Commander {
 public:
  Status Execute([[maybe_unused]] engine::Context &ctx, [[maybe_unused]] Server *srv, Connection *conn,
                 std::string *output) override {
    if (args_.size() == 1) {
      CommandTable::GetAllCommandsInfo(output);
      return Status::OK();
    }

    auto sub_command = util::ToLower(args_[1]);
    if (sub_command == "count" && args_.size() == 2) {
      *output = redis::Integer(CommandTable::Size());
    } else if (sub_command == "info" && args_.size() >= 3) {
      CommandTable::GetCommandsInfo(output, std::vector<std::string>(args_.begin() + 2, args_.end()));
    } else if (sub_command == "getkeys" && args_.size() >= 3) {
      auto cmd_iter = CommandTable::Get()->find(util::ToLower(args_[2]));
      if (cmd_iter == CommandTable::Get()->end()) return {Status::RedisUnknownCmd, "Invalid command specified"};
      auto key_indexes = GET_OR_RET(
          CommandTable::GetKeysFromCommand(cmd_iter->second, std::vector<std::string>(args_.begin() + 2, args_.end())));
      if (key_indexes.empty()) return {Status::RedisExecErr, "Invalid arguments specified for command"};
      std::vector<std::string> keys;
      keys.reserve(key_indexes.size());
      for (const auto &key_index : key_indexes) {
        keys.emplace_back(args_[key_index + 2]);
      }
      *output = conn->MultiBulkString(keys);
    } else {
      return {Status::RedisExecErr, "Command subcommand must be one of COUNT, GETKEYS, INFO"};
    }
    return Status::OK();
  }
};

class CommandEcho : public Commander {
 public:
  Status Execute([[maybe_unused]] engine::Context &ctx, [[maybe_unused]] Server *srv, [[maybe_unused]] Connection *conn,
                 std::string *output) override {
    *output = redis::BulkString(args_[1]);
    return Status::OK();
  }
};

class CommandHello final : public Commander {
 public:
  Status Execute([[maybe_unused]] engine::Context &ctx, Server *srv, Connection *conn, std::string *output) override {
    size_t next_arg = 1;
    int protocol = 2;
    if (args_.size() >= 2) {
      auto parse_result = ParseInt<int>(args_[next_arg], 10);
      ++next_arg;
      if (!parse_result) return {Status::NotOK, "Protocol version is not an integer or out of range"};
      protocol = *parse_result;
      if (protocol < 2 || protocol > 3) return {Status::RedisNoProto, "unsupported protocol version"};
    }

    for (; next_arg < args_.size(); ++next_arg) {
      size_t more_args = args_.size() - next_arg - 1;
      const std::string &opt = args_[next_arg];
      if (util::EqualICase(opt, "auth") && more_args != 0) {
        if (more_args == 2 || more_args == 4) {
          if (args_[next_arg + 1] != "default") return {Status::NotOK, "Invalid password"};
          next_arg++;
        }
        const auto &user_password = args_[next_arg + 1];
        std::string ns;
        AuthResult auth_result = srv->AuthenticateUser(user_password, &ns);
        switch (auth_result) {
          case AuthResult::NO_REQUIRE_PASS:
            return {Status::NotOK, "Client sent AUTH, but no password is set"};
          case AuthResult::INVALID_PASSWORD:
            return {Status::NotOK, "Invalid password"};
          case AuthResult::IS_USER:
            conn->BecomeUser();
            break;
          case AuthResult::IS_ADMIN:
            conn->BecomeAdmin();
            break;
        }
        conn->SetNamespace(ns);
        next_arg += 1;
      } else if (util::EqualICase(opt, "setname") && more_args != 0) {
        conn->SetName(args_[next_arg + 1]);
        next_arg += 1;
      } else {
        return {Status::RedisExecErr, "Syntax error in HELLO option " + opt};
      }
    }

    std::vector<std::string> output_list;
    output_list.push_back(redis::BulkString("server"));
    output_list.push_back(redis::BulkString("redis"));
    output_list.push_back(redis::BulkString("version"));
    output_list.push_back(redis::BulkString(REDIS_VERSION));
    output_list.push_back(redis::BulkString("proto"));
    if (srv->GetConfig()->resp3_enabled) {
      output_list.push_back(redis::Integer(protocol));
      conn->SetProtocolVersion(protocol == 3 ? RESP::v3 : RESP::v2);
    } else {
      output_list.push_back(redis::Integer(2));
    }
    output_list.push_back(redis::BulkString("mode"));
    output_list.push_back(redis::BulkString("standalone"));
    output_list.push_back(redis::BulkString("role"));
    output_list.push_back(redis::BulkString("master"));
    output_list.push_back(redis::BulkString("modules"));
    output_list.push_back(conn->NilArray());

    *output = conn->HeaderOfMap(output_list.size() / 2);
    for (const auto &item : output_list) {
      *output += item;
    }
    return Status::OK();
  }
};

REDIS_REGISTER_COMMANDS(Server, MakeCmdAttr<CommandAuth>("auth", 2, "read-only ok-loading auth", NO_KEY),
                        MakeCmdAttr<CommandPing>("ping", -1, "read-only", NO_KEY),
                        MakeCmdAttr<CommandInfo>("info", -1, "read-only ok-loading", NO_KEY),
                        MakeCmdAttr<CommandDBSize>("dbsize", -1, "read-only", NO_KEY),
                        MakeCmdAttr<CommandClient>("client", -2, "read-only", NO_KEY),
                        MakeCmdAttr<CommandCommand>("command", -1, "read-only", NO_KEY),
                        MakeCmdAttr<CommandEcho>("echo", 2, "read-only", NO_KEY),
                        MakeCmdAttr<CommandHello>("hello", -1, "read-only ok-loading auth", NO_KEY), )

}  // namespace redis
