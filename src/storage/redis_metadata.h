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

#pragma once

#include <rocksdb/status.h>
#include <sys/time.h>

#include <atomic>
#include <array>
#include <bitset>
#include <initializer_list>
#include <limits>
#include <string>
#include <vector>

#include "encoding.h"

constexpr bool USE_64BIT_COMMON_FIELD_DEFAULT = METADATA_ENCODING_VERSION != 0;

// Keep historical type ids stable so XRocksCache can recognize old metadata
// and return WRONGTYPE instead of mis-reading unsupported values as strings.
enum RedisType : uint8_t {
  kRedisNone = 0,
  kRedisString = 1,
  kRedisHash = 2,
  kRedisList = 3,
  kRedisSet = 4,
  kRedisZSet = 5,
  kRedisBitmap = 6,
  kRedisSortedint = 7,
  kRedisStream = 8,
  kRedisBloomFilter = 9,
  kRedisJson = 10,
  kRedisHyperLogLog = 11,
  kRedisTDigest = 12,
  kRedisTimeSeries = 13,
  kRedisCuckooFilter = 14,
  kRedisTypeMax
};

inline constexpr const std::array<std::string_view, kRedisTypeMax> RedisTypeNames = {
    "none",   "string",    "hash",      "list",        "set",       "zset",       "bitmap",   "sortedint",
    "stream", "MBbloom--", "ReJSON-RL", "hyperloglog", "TDIS-TYPE", "timeseries", "MBbloomCF"};

struct RedisTypes {
  RedisTypes(std::initializer_list<RedisType> list) {
    for (auto type : list) {
      types_.set(type);
    }
  }

  static RedisTypes All() {
    UnderlyingType types;
    types.set();
    return RedisTypes(types);
  }

  bool Contains(RedisType type) const { return types_[type]; }

 private:
  using UnderlyingType = std::bitset<128>;

  explicit RedisTypes(UnderlyingType types) : types_(types) {}

  UnderlyingType types_;
};

enum RedisCommand {
  kRedisCmdLSet,
  kRedisCmdLInsert,
  kRedisCmdLTrim,
  kRedisCmdLPop,
  kRedisCmdRPop,
  kRedisCmdLRem,
  kRedisCmdLPush,
  kRedisCmdRPush,
  kRedisCmdExpire,
  kRedisCmdSetBit,
  kRedisCmdBitOp,
  kRedisCmdBitfield,
  kRedisCmdLMove,
};

constexpr const char *kErrMsgWrongType = "WRONGTYPE Operation against a key holding the wrong kind of value";
constexpr const char *kErrMsgKeyExpired = "the key was expired";

using rocksdb::Slice;

struct KeyNumStats {
  uint64_t n_key = 0;
  uint64_t n_expires = 0;
  uint64_t n_expired = 0;
  uint64_t avg_ttl = 0;
};

[[nodiscard]] uint16_t ExtractSlotId(Slice ns_key);
template <typename T = Slice>
[[nodiscard]] std::tuple<T, T> ExtractNamespaceKey(Slice ns_key, bool slot_id_encoded);
[[nodiscard]] std::string ComposeNamespaceKey(const Slice &ns, const Slice &key, bool slot_id_encoded);
[[nodiscard]] std::string ComposeSlotKeyPrefix(const Slice &ns, int slotid);
[[nodiscard]] std::string ComposeSlotKeyUpperBound(const Slice &ns, int slotid);

class InternalKey {
 public:
  explicit InternalKey(Slice ns_key, Slice sub_key, uint64_t version, bool slot_id_encoded);
  explicit InternalKey(Slice input, bool slot_id_encoded);
  ~InternalKey() = default;

  Slice GetNamespace() const;
  Slice GetKey() const;
  Slice GetSubKey() const;
  uint64_t GetVersion() const;
  [[nodiscard]] std::string Encode() const;
  bool operator==(const InternalKey &that) const;

 private:
  Slice namespace_;
  Slice key_;
  Slice sub_key_;
  uint64_t version_;
  uint16_t slotid_;
  bool slot_id_encoded_;
};

constexpr uint8_t METADATA_64BIT_ENCODING_MASK = 0x80;
constexpr uint8_t METADATA_TYPE_MASK = 0x0f;

class Metadata {
 public:
  // metadata flags
  // <(1-bit) 64bit-common-field-indicator> 0 0 0 <(4-bit) redis-type>
  // 64bit-common-field-indicator: make `expire` and `size` 64bit instead of 32bit
  // NOTE: `expire` is stored in milliseconds for 64bit, seconds for 32bit
  // redis-type: RedisType for the key-value
  uint8_t flags;

  // expire timestamp, in milliseconds
  uint64_t expire;

  // the current version: 53bit timestamp + 11bit counter
  uint64_t version;

  // element size of the key-value
  uint64_t size;

  explicit Metadata(RedisType type, bool generate_version = true,
                    bool use_64bit_common_field = USE_64BIT_COMMON_FIELD_DEFAULT);

  static void InitVersionCounter();

  static size_t GetOffsetAfterExpire(uint8_t flags);
  static size_t GetOffsetAfterSize(uint8_t flags);
  static uint64_t ExpireMsToS(uint64_t ms);

  bool Is64BitEncoded() const;
  bool GetFixedCommon(rocksdb::Slice *input, uint64_t *value) const;
  bool GetExpire(rocksdb::Slice *input);
  void PutFixedCommon(std::string *dst, uint64_t value) const;
  void PutExpire(std::string *dst) const;

  RedisType Type() const;
  std::string_view TypeName() const;
  size_t CommonEncodedSize() const;
  int64_t TTL() const;
  timeval Time() const;
  bool Expired() const;
  bool ExpireAt(uint64_t expired_ts) const;

  // return whether for this type, the metadata itself is the whole data,
  // no other key-values.
  // this means that the metadata of these types do NOT have
  // `version` and `size` field.
  // e.g. RedisString. RedisJson is kept only for historical metadata decoding.
  bool IsSingleKVType() const;

  // return whether the `size` field of this type can be zero.
  // if a type is NOT an emptyable type,
  // any key of this type is regarded as expired if `size` equals to 0.
  // e.g. any SingleKVType. Historical emptyable complex types are recognized
  // only so unsupported legacy keys are not incorrectly treated as expired.
  bool IsEmptyableType() const;

  virtual void Encode(std::string *dst) const;
  [[nodiscard]] virtual rocksdb::Status Decode(Slice *input);
  [[nodiscard]] rocksdb::Status Decode(Slice input);

  bool operator==(const Metadata &that) const;
  virtual ~Metadata() = default;

 private:
  static uint64_t generateVersion();
};
