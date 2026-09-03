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
 *
 */

#include "commander.h"

#include <unordered_set>

#include "cluster/redis_slot.h"
#include "common/string_util.h"
#include "server/redis_reply.h"

namespace redis {

RegisterToCommandTable::RegisterToCommandTable(CommandCategory category,
                                               std::initializer_list<CommandAttributes> list) {
  if (category == CommandCategory::Disabled) {
    return;
  }

  for (auto attr : list) {
    attr.category = category;
    CommandTable::redis_command_table.emplace_back(attr);
    CommandTable::original_commands[attr.name] = &CommandTable::redis_command_table.back();
    CommandTable::commands[attr.name] = &CommandTable::redis_command_table.back();
  }
}

size_t CommandTable::Size() { return commands.size(); }

const CommandMap *CommandTable::GetOriginal() { return &original_commands; }

CommandMap *CommandTable::Get() { return &commands; }

void CommandTable::Reset() {
  commands = original_commands;
  profile_disabled_commands.clear();
}

void CommandTable::EnableXRocksCacheProfile() {
  static const std::unordered_set<std::string> supported_commands = {
      "auth",   "client", "command", "dbsize", "decr",    "decrby", "del",     "echo",
      "exists", "expire", "get",     "hello",  "incr",    "incrby", "info",    "mget",
      "mset",   "pexpire", "ping",    "pttl",   "set",     "ttl",
  };

  // Commands outside XRocksCache's K/V cache scope.
  // Seeding them here keeps the friendly "command not supported" reply instead of "unknown command".
  static const std::unordered_set<std::string> removed_commands = {
      "_db_name", "_fetch_file", "_fetch_meta", "applybatch", "asking", "bf.add", "bf.card", "bf.exists",
      "bf.info", "bf.insert", "bf.madd", "bf.mexists", "bf.reserve", "bgsave", "bitcount", "bitop",
      "bitpos", "blmove", "blmpop", "blpop", "brpop", "bzmpop", "bzpopmax", "bzpopmin",
      "cas", "cf.add", "cf.reserve", "cluster", "clusterx", "compact", "config", "copy",
      "debug", "delex", "digest", "discard", "disk", "dump", "eval", "eval_ro",
      "evalsha", "evalsha_ro", "exec", "expireat", "expiretime", "flushall", "flushbackup", "flushblockcache",
      "flushdb", "flushmemtable", "ft._list", "ft.create", "ft.dropindex", "ft.explain", "ft.explainsql", "ft.info",
      "ft.search", "ft.searchsql", "ft.tagvals", "function", "geoadd", "geodist", "geohash", "geopos",
      "georadius", "georadius_ro", "georadiusbymember", "georadiusbymember_ro", "geosearch", "geosearchstore", "getbit", "getdel",
      "getex", "getrange", "getset", "hdel", "hexists", "hget", "hgetall", "hgetex",
      "hincrby", "hincrbyfloat", "hkeys", "hlen", "hmget", "hmset", "hpersist", "hrandfield",
      "hrangebylex", "hscan", "hset", "hsetex", "hsetexpire", "hsetnx", "hstrlen", "hvals",
      "incrbyfloat", "json.arrappend", "json.arrindex", "json.arrinsert", "json.arrlen", "json.arrpop", "json.arrtrim", "json.clear",
      "json.debug", "json.del", "json.forget", "json.get", "json.info", "json.merge", "json.mget", "json.mset",
      "json.numincrby", "json.nummultby", "json.objkeys", "json.objlen", "json.resp", "json.set", "json.strappend", "json.strlen",
      "json.toggle", "json.type", "keys", "kmetadata", "kprofile", "lastsave", "latency", "lcs",
      "lindex", "linsert", "llen", "lmove", "lmpop", "lpop", "lpos", "lpush",
      "lpushx", "lrange", "lrem", "lset", "ltrim", "memory", "monitor", "move",
      "movex", "mpublish", "msetex", "msetnx", "multi", "namespace", "object", "perflog",
      "persist", "pexpireat", "pexpiretime", "pfadd", "pfcount", "pfmerge", "pollupdates", "psetex",
      "psubscribe", "psync", "publish", "pubsub", "punsubscribe", "randomkey", "rdb", "readonly",
      "readwrite", "rename", "renamenx", "replconf", "replicaof", "reset", "restore", "role",
      "rpop", "rpoplpush", "rpush", "rpushx", "sadd", "scan", "scard", "script",
      "sdiff", "sdiffstore", "select", "setbit", "setnx", "setrange", "shutdown", "siadd",
      "sicard", "siexists", "sinter", "sintercard", "sinterstore", "sirange", "sirangebyvalue", "sirem",
      "sirevrange", "sirevrangebyvalue", "sismember", "slaveof", "slowlog", "smembers", "smismember", "smove",
      "spop", "srandmember", "srem", "sscan", "sst", "ssubscribe", "stats", "strlen",
      "subscribe", "substr", "sunion", "sunionstore", "sunsubscribe", "tdigest.add", "tdigest.byrank", "tdigest.byrevrank",
      "tdigest.cdf", "tdigest.create", "tdigest.info", "tdigest.max", "tdigest.merge", "tdigest.min", "tdigest.quantile", "tdigest.rank",
      "tdigest.reset", "tdigest.revrank", "tdigest.trimmed_mean", "time", "ts.add", "ts.alter", "ts.create", "ts.createrule",
      "ts.decrby", "ts.del", "ts.get", "ts.incrby", "ts.info", "ts.madd", "ts.mget", "ts.mrange",
      "ts.mrevrange", "ts.queryindex", "ts.range", "ts.revrange", "type", "unlink", "unsubscribe", "unwatch",
      "wait", "watch", "xack", "xadd", "xautoclaim", "xclaim", "xdel", "xgroup",
      "xinfo", "xlen", "xpending", "xrange", "xread", "xreadgroup", "xrevrange", "xsetid",
      "xtrim", "zadd", "zcard", "zcount", "zdiff", "zdiffstore", "zincrby", "zinter",
      "zintercard", "zinterstore", "zlexcount", "zmpop", "zmscore", "zpopmax", "zpopmin", "zrandmember",
      "zrange", "zrangebylex", "zrangebyscore", "zrangestore", "zrank", "zrem", "zremrangebylex", "zremrangebyrank",
      "zremrangebyscore", "zrevrange", "zrevrangebylex", "zrevrangebyscore", "zrevrank", "zscan", "zscore", "zunion",
      "zunionstore",
  };

  profile_disabled_commands = removed_commands;
  for (auto iter = commands.begin(); iter != commands.end();) {
    if (supported_commands.contains(iter->second->name)) {
      ++iter;
      continue;
    }

    profile_disabled_commands.insert(iter->first);
    profile_disabled_commands.insert(iter->second->name);
    iter = commands.erase(iter);
  }
}

bool CommandTable::IsDisabledByProfile(const std::string &name) {
  return profile_disabled_commands.contains(util::ToLower(name));
}

std::string CommandTable::GetCommandInfo(const CommandAttributes *command_attributes) {
  std::string command, command_flags;
  command.append(redis::MultiLen(6));
  command.append(redis::BulkString(command_attributes->name));
  command.append(redis::Integer(command_attributes->arity));
  command.append(redis::ArrayOfBulkStrings(CommandAttributes::FlagsToString(command_attributes->InitialFlags())));
  auto key_range = command_attributes->InitialKeyRange().ValueOr({0, 0, 0});
  command.append(redis::Integer(key_range.first_key));
  command.append(redis::Integer(key_range.last_key));
  command.append(redis::Integer(key_range.key_step));
  return command;
}

void CommandTable::GetAllCommandsInfo(std::string *info) {
  info->append(redis::MultiLen(commands.size()));
  for (const auto &iter : commands) {
    auto command_attribute = iter.second;
    auto command_info = GetCommandInfo(command_attribute);
    info->append(command_info);
  }
}

void CommandTable::GetCommandsInfo(std::string *info, const std::vector<std::string> &cmd_names) {
  info->append(redis::MultiLen(cmd_names.size()));
  for (const auto &cmd_name : cmd_names) {
    auto cmd_iter = commands.find(util::ToLower(cmd_name));
    if (cmd_iter == commands.end()) {
      info->append(NilString(RESP::v2));
    } else {
      auto command_attribute = cmd_iter->second;
      auto command_info = GetCommandInfo(command_attribute);
      info->append(command_info);
    }
  }
}

StatusOr<std::vector<int>> CommandTable::GetKeysFromCommand(const CommandAttributes *attributes,
                                                            const std::vector<std::string> &cmd_tokens) {
  int argc = static_cast<int>(cmd_tokens.size());

  if (!attributes->CheckArity(argc)) {
    return {Status::NotOK, "Invalid number of arguments specified for command"};
  }

  auto cmd = attributes->factory();
  if (auto s = cmd->Parse(cmd_tokens); !s) {
    return {Status::NotOK, "Invalid syntax found in this command arguments: " + s.Msg()};
  }

  Status status;
  std::vector<int> key_indexes;

  attributes->ForEachKeyRange(
      [&](const std::vector<std::string> &, CommandKeyRange key_range) {
        key_range.ForEachKeyIndex([&](int i) { key_indexes.push_back(i); }, cmd_tokens.size());
      },
      cmd_tokens, [&](const auto &) { status = {Status::NotOK, "The command has no key arguments"}; });

  if (!status) {
    return status;
  }

  return key_indexes;
}

bool CommandTable::IsExists(const std::string &name) {
  return original_commands.find(util::ToLower(name)) != original_commands.end();
}

Status CommandTable::ParseSlotRanges(const std::string &slots_str, std::vector<SlotRange> &slots) {
  if (slots_str.empty()) {
    return {Status::NotOK, "No slots to parse."};
  }

  std::vector<std::string> slot_ranges = util::Split(slots_str, " ");
  if (slot_ranges.empty()) {
    return {Status::NotOK,
            fmt::format("Invalid slots: `{}`. No slots to parse. Please use spaces to separate slots.", slots_str)};
  }

  auto valid_range = NumericRange<int>{0, kClusterSlots - 1};
  // Parse all slots (include slot ranges)
  for (auto &slot_range : slot_ranges) {
    if (slot_range.find('-') == std::string::npos) {
      auto parse_result = ParseInt<int>(slot_range, valid_range, 10);
      if (!parse_result) {
        return std::move(parse_result).Prefixed(errInvalidSlotID);
      }
      slots.emplace_back(*parse_result, *parse_result);
      continue;
    }

    // parse slot range: "int1-int2" (satisfy: int1 <= int2 )
    if (slot_range.front() == '-' || slot_range.back() == '-') {
      return {Status::NotOK,
              fmt::format("Invalid slot range: `{}`. The character '-' can't appear in the first or last position.",
                          slot_range)};
    }
    std::vector<std::string> fields = util::Split(slot_range, "-");
    if (fields.size() != 2) {
      return {Status::NotOK,
              fmt::format("Invalid slot range: `{}`. The slot range should be of the form `int1-int2`.", slot_range)};
    }
    auto parse_start = ParseInt<int>(fields[0], valid_range, 10);
    auto parse_end = ParseInt<int>(fields[1], valid_range, 10);
    if (!parse_start || !parse_end || *parse_start > *parse_end) {
      return {Status::NotOK,
              fmt::format(
                  "Invalid slot range: `{}`. The slot range `int1-int2` needs to satisfy the condition (int1 <= int2).",
                  slot_range)};
    }
    slots.emplace_back(*parse_start, *parse_end);
  }

  return Status::OK();
}

}  // namespace redis
