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

#include "compact_filter.h"

#include <string>
#include <utility>

#include "db_util.h"
#include "logging.h"
#include "storage/redis_metadata.h"
#include "time_util.h"

namespace engine {

bool MetadataFilter::Filter([[maybe_unused]] int level, const rocksdb::Slice &key, const rocksdb::Slice &value,
                            [[maybe_unused]] std::string *new_value, [[maybe_unused]] bool *modified) const {
  Metadata metadata(kRedisNone, false);
  rocksdb::Status s = metadata.Decode(value);
  auto [ns, user_key] = ExtractNamespaceKey(key, stor_->IsSlotIdEncoded());
  if (!s.ok()) {
    WARN("[compact_filter/metadata] Failed to decode, namespace: {}, key: {}, err: {}", ns, user_key, s.ToString());
    return false;
  }

  bool expired = metadata.Expired();
  DEBUG("[compact_filter/metadata] namespace: {}, key: {}, result: {}", ns, user_key,
        (expired ? "deleted" : "reserved"));
  return expired;
}

Status SubKeyFilter::GetMetadata(const InternalKey &ikey, Metadata *metadata) const {
  auto db = stor_->GetDB();
  if (!db || stor_->GetCFHandles()->size() < 2) return {Status::NotOK, "storage is closed"};
  std::string metadata_key = ComposeNamespaceKey(ikey.GetNamespace(), ikey.GetKey(), stor_->IsSlotIdEncoded());

  if (cached_key_.empty() || metadata_key != cached_key_) {
    std::string bytes;
    rocksdb::Status s =
        db->Get(rocksdb::ReadOptions(), stor_->GetCFHandle(ColumnFamilyID::Metadata), metadata_key, &bytes);
    cached_key_ = std::move(metadata_key);
    if (s.ok()) {
      cached_metadata_ = std::move(bytes);
    } else if (s.IsNotFound()) {
      cached_metadata_.clear();
      return {Status::NotFound, "metadata is not found"};
    } else {
      cached_key_.clear();
      cached_metadata_.clear();
      return {Status::NotOK, "fetch error: " + s.ToString()};
    }
  }

  if (cached_metadata_.empty()) return {Status::NotFound, "metadata is not found"};
  rocksdb::Status s = metadata->Decode(cached_metadata_);
  if (!s.ok()) {
    cached_key_.clear();
    return {Status::NotOK, "decode error: " + s.ToString()};
  }
  return Status::OK();
}

bool SubKeyFilter::IsMetadataExpired(const InternalKey &ikey, const Metadata &metadata) {
  // Lazy delete gives writers a short safety window around EXPIRE/SET races.
  uint64_t lazy_expired_ts = util::GetTimeStampMS() - 300000;
  return metadata.IsSingleKVType() || metadata.ExpireAt(lazy_expired_ts) || ikey.GetVersion() != metadata.version;
}

rocksdb::CompactionFilter::Decision SubKeyFilter::FilterBlobByKey([[maybe_unused]] int level,
                                                                  const rocksdb::Slice &key,
                                                                  [[maybe_unused]] std::string *new_value,
                                                                  [[maybe_unused]] std::string *skip_until) const {
  InternalKey ikey(key, stor_->IsSlotIdEncoded());
  Metadata metadata(kRedisNone, false);
  Status s = GetMetadata(ikey, &metadata);
  if (s.Is<Status::NotFound>()) {
    return rocksdb::CompactionFilter::Decision::kRemove;
  }
  if (!s.IsOK()) {
    ERROR("[compact_filter/subkey] Failed to get metadata, namespace: {}, key: {}, err: {}", ikey.GetNamespace(),
          ikey.GetKey(), s.Msg());
    return rocksdb::CompactionFilter::Decision::kKeep;
  }

  bool result = IsMetadataExpired(ikey, metadata);
  return result ? rocksdb::CompactionFilter::Decision::kRemove : rocksdb::CompactionFilter::Decision::kKeep;
}

bool SubKeyFilter::Filter([[maybe_unused]] int level, const rocksdb::Slice &key,
                          [[maybe_unused]] const rocksdb::Slice &value, [[maybe_unused]] std::string *new_value,
                          [[maybe_unused]] bool *modified) const {
  InternalKey ikey(key, stor_->IsSlotIdEncoded());
  Metadata metadata(kRedisNone, false);
  Status s = GetMetadata(ikey, &metadata);
  if (s.Is<Status::NotFound>()) {
    return true;
  }
  if (!s.IsOK()) {
    ERROR("[compact_filter/subkey] Failed to get metadata, namespace: {}, key: {}, err: {}", ikey.GetNamespace(),
          ikey.GetKey(), s.Msg());
    return false;
  }

  return IsMetadataExpired(ikey, metadata);
}

}  // namespace engine
