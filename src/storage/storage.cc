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

#include "storage.h"

#include <event2/buffer.h>
#include <fcntl.h>
#include <rocksdb/convenience.h>
#include <rocksdb/env.h>
#include <rocksdb/filter_policy.h>
#include <rocksdb/rate_limiter.h>
#include <rocksdb/sst_file_manager.h>
#include <rocksdb/utilities/checkpoint.h>
#include <rocksdb/utilities/table_properties_collectors.h>

#include <algorithm>
#include <iostream>
#include <memory>
#include <random>
#include <string_view>

#include "compact_filter.h"
#include "db_util.h"
#include "event_listener.h"
#include "event_util.h"
#include "logging.h"
#include "redis_db.h"
#include "redis_metadata.h"
#include "rocksdb/cache.h"
#include "rocksdb/options.h"
#include "rocksdb/write_batch.h"
#include "rocksdb_crc32c.h"
#include "server/server.h"
#include "storage/batch_indexer.h"
#include "string_util.h"
#include "table_properties_collector.h"
#include "time_util.h"
#include "unique_fd.h"

namespace engine {

constexpr int kRocksdbLRUAutoAdjustShardBits = -1;

// used as the default argument for `strict_capacity_limit` in creating rocksdb::Cache.
constexpr bool kRocksdbCacheStrictCapacityLimit = false;

// used as the default argument for `high_pri_pool_ratio` in creating block cache.
constexpr double kRocksdbLRUBlockCacheHighPriPoolRatio = 0.75;

// used in creating rocksdb::HyperClockCache, set`estimated_entry_charge` to 0 means let rocksdb dynamically and
// automatically adjust the table size for the cache.
constexpr size_t kRockdbHCCAutoAdjustCharge = 0;

const int64_t kIORateLimitMaxMb = 1024000;

using rocksdb::Slice;

Storage::Storage(Config *config)
    : backup_creating_time_secs_(util::GetTimeStamp<std::chrono::seconds>()),
      env_(rocksdb::Env::Default()),
      config_(config),
      lock_mgr_(16),
      db_stats_(std::make_unique<DBStats>()) {
  Metadata::InitVersionCounter();
  SetWriteOptions(config->rocks_db.write_options);
}

Storage::~Storage() {
  CloseDB();
  TrySkipBlockCacheDeallocationOnClose();
}

void Storage::TrySkipBlockCacheDeallocationOnClose() {
  if (config_->skip_block_cache_deallocation_on_close) {
    shared_block_cache_->DisownData();
  }
}

void Storage::CloseDB() {
  auto guard = WriteLockGuard();
  if (!db_) return;

  db_closing_ = true;
  db_->SyncWAL();
  // Make sure all background work is stopped to avoid the data race
  // between background threads and the column family handle destruction.
  rocksdb::CancelAllBackgroundWork(db_.get(), true);
  for (auto handle : cf_handles_) db_->DestroyColumnFamilyHandle(handle);
  db_->Close();
  db_ = nullptr;
}

void Storage::SetWriteOptions(const Config::RocksDB::WriteOptions &config) {
  default_write_opts_.sync = config.sync;
  default_write_opts_.disableWAL = config.disable_wal;
  default_write_opts_.no_slowdown = config.no_slowdown;
  default_write_opts_.low_pri = config.low_pri;
  default_write_opts_.memtable_insert_hint_per_batch = config.memtable_insert_hint_per_batch;
}

rocksdb::ReadOptions Storage::DefaultScanOptions() const {
  rocksdb::ReadOptions read_options;
  read_options.fill_cache = false;
  read_options.async_io = config_->rocks_db.read_options.async_io;

  return read_options;
}

rocksdb::ReadOptions Storage::DefaultMultiGetOptions() const {
  rocksdb::ReadOptions read_options;
  read_options.async_io = config_->rocks_db.read_options.async_io;

  return read_options;
}

rocksdb::BlockBasedTableOptions Storage::InitTableOptions() {
  rocksdb::BlockBasedTableOptions table_options;
  table_options.format_version = 5;
  table_options.index_type = rocksdb::BlockBasedTableOptions::IndexType::kTwoLevelIndexSearch;
  table_options.filter_policy.reset(rocksdb::NewBloomFilterPolicy(10, false));
  table_options.partition_filters = config_->rocks_db.partition_filters;
  table_options.optimize_filters_for_memory = true;
  table_options.metadata_block_size = 4096;
  table_options.data_block_index_type = rocksdb::BlockBasedTableOptions::DataBlockIndexType::kDataBlockBinaryAndHash;
  table_options.data_block_hash_table_util_ratio = 0.75;
  table_options.block_size = static_cast<size_t>(config_->rocks_db.block_size);
  return table_options;
}

void Storage::SetBlobDB(rocksdb::ColumnFamilyOptions *cf_options) {
  cf_options->enable_blob_files = config_->rocks_db.enable_blob_files;
  cf_options->blob_cache = config_->enable_blob_cache ? shared_block_cache_ : nullptr;
  cf_options->min_blob_size = config_->rocks_db.min_blob_size;
  cf_options->blob_file_size = config_->rocks_db.blob_file_size;
  cf_options->blob_compression_type = config_->rocks_db.compression;
  cf_options->enable_blob_garbage_collection = config_->rocks_db.enable_blob_garbage_collection;
  // Use 100.0 to force converting blob_garbage_collection_age_cutoff to double
  cf_options->blob_garbage_collection_age_cutoff = config_->rocks_db.blob_garbage_collection_age_cutoff / 100.0;
}

rocksdb::Options Storage::InitRocksDBOptions() {
  rocksdb::Options options;
  options.create_if_missing = true;
  options.create_missing_column_families = true;
  // options.IncreaseParallelism(2);
  // NOTE: the overhead of statistics is 5%-10%, so it should be configurable in prod env
  // See: https://github.com/facebook/rocksdb/wiki/Statistics
  options.statistics = rocksdb::CreateDBStatistics();
  options.stats_dump_period_sec = config_->rocks_db.stats_dump_period_sec;
  options.max_open_files = config_->rocks_db.max_open_files;
  options.compaction_style = rocksdb::CompactionStyle::kCompactionStyleLevel;
  options.max_subcompactions = static_cast<uint32_t>(config_->rocks_db.max_subcompactions);
  options.max_background_flushes = config_->rocks_db.max_background_flushes;
  options.max_background_compactions = config_->rocks_db.max_background_compactions;
  options.max_write_buffer_number = config_->rocks_db.max_write_buffer_number;
  options.min_write_buffer_number_to_merge = config_->rocks_db.min_write_buffer_number_to_merge;
  options.write_buffer_size = config_->rocks_db.write_buffer_size * MiB;
  options.num_levels = XROCKSCACHE_MAX_LSM_LEVEL;
  options.compression_opts.level = config_->rocks_db.compression_level;
  options.compression_opts.max_dict_bytes = config_->rocks_db.compression_max_dict_bytes;
  options.compression_opts.zstd_max_train_bytes = config_->rocks_db.compression_zstd_max_train_bytes;
  options.compression_per_level.resize(options.num_levels);
  options.wal_compression = config_->rocks_db.wal_compression;
  for (int i = 0; i < options.num_levels; ++i) {
    if (i < config_->rocks_db.compression_start_level) {
      options.compression_per_level[i] = rocksdb::CompressionType::kNoCompression;
    } else {
      options.compression_per_level[i] = config_->rocks_db.compression;
    }
  }

  options.enable_pipelined_write = config_->rocks_db.enable_pipelined_write;
  options.target_file_size_base = config_->rocks_db.target_file_size_base * MiB;
  options.max_manifest_file_size = 64 * MiB;
  options.max_log_file_size = 256 * MiB;
  options.keep_log_file_num = 12;
  options.WAL_ttl_seconds = static_cast<uint64_t>(config_->rocks_db.wal_ttl_seconds);
  options.WAL_size_limit_MB = static_cast<uint64_t>(config_->rocks_db.wal_size_limit_mb);
  options.max_total_wal_size = static_cast<uint64_t>(config_->rocks_db.max_total_wal_size * MiB);
  options.listeners.emplace_back(new EventListener(this));
  options.dump_malloc_stats = config_->rocks_db.dump_malloc_stats;
  sst_file_manager_ = std::shared_ptr<rocksdb::SstFileManager>(rocksdb::NewSstFileManager(
      rocksdb::Env::Default(), nullptr, "", config_->rocks_db.sst_file_delete_rate_bytes_per_sec));
  options.sst_file_manager = sst_file_manager_;
  int64_t max_io_mb = kIORateLimitMaxMb;
  if (config_->max_io_mb > 0) max_io_mb = config_->max_io_mb;

  rate_limiter_ = std::shared_ptr<rocksdb::RateLimiter>(rocksdb::NewGenericRateLimiter(
      max_io_mb * static_cast<int64_t>(MiB), 100 * 1000, /* default */
      10,                                                /* default */
      rocksdb::RateLimiter::Mode::kWritesOnly, config_->rocks_db.rate_limiter_auto_tuned));

  options.rate_limiter = rate_limiter_;
  options.delayed_write_rate = static_cast<uint64_t>(config_->rocks_db.delayed_write_rate);
  options.compaction_readahead_size = static_cast<size_t>(config_->rocks_db.compaction_readahead_size);
  options.level0_slowdown_writes_trigger = config_->rocks_db.level0_slowdown_writes_trigger == 0
                                               ? config_->rocks_db.level0_stop_writes_trigger
                                               : config_->rocks_db.level0_slowdown_writes_trigger;
  options.level0_stop_writes_trigger = config_->rocks_db.level0_stop_writes_trigger;
  options.level0_file_num_compaction_trigger = config_->rocks_db.level0_file_num_compaction_trigger;
  options.max_bytes_for_level_base = config_->rocks_db.max_bytes_for_level_base;
  options.max_bytes_for_level_multiplier = config_->rocks_db.max_bytes_for_level_multiplier;
  options.level_compaction_dynamic_level_bytes = config_->rocks_db.level_compaction_dynamic_level_bytes;
  options.max_background_jobs = config_->rocks_db.max_background_jobs;
  options.max_compaction_bytes = static_cast<uint64_t>(config_->rocks_db.max_compaction_bytes);
  options.periodic_compaction_seconds = config_->rocks_db.periodic_compaction_seconds;
  options.ttl = config_->rocks_db.ttl;
  options.daily_offpeak_time_utc = config_->rocks_db.daily_offpeak_time_utc;

  // avoid blocking io on iteration
  // see https://github.com/facebook/rocksdb/wiki/IO#avoid-blocking-io
  options.avoid_unnecessary_blocking_io = config_->rocks_db.avoid_unnecessary_blocking_io;
  return options;
}

Status Storage::SetOptionForAllColumnFamilies(const std::string &key, const std::string &value) {
  return SetOptionForAllColumnFamilies({{key, value}});
}

Status Storage::SetOptionForAllColumnFamilies(const std::unordered_map<std::string, std::string> &options_map) {
  for (auto &cf_handle : cf_handles_) {
    auto s = db_->SetOptions(cf_handle, options_map);
    if (!s.ok()) return {Status::NotOK, s.ToString()};
  }
  return Status::OK();
}

Status Storage::SetDBOption(const std::string &key, const std::string &value) {
  auto s = db_->SetDBOptions({{key, value}});
  if (!s.ok()) return {Status::NotOK, s.ToString()};
  return Status::OK();
}

Status Storage::CreateColumnFamilies(const rocksdb::Options &options) {
  rocksdb::ColumnFamilyOptions cf_options(options);
  auto res = util::DBOpen(options, config_->db_dir);
  if (res) {
    std::vector<std::string> cf_names_except_default;
    for (const auto &cf : ColumnFamilyConfigs::ListColumnFamiliesWithoutDefault()) {
      cf_names_except_default.emplace_back(cf.Name());
    }
    std::vector<rocksdb::ColumnFamilyHandle *> cf_handles;
    auto s = (*res)->CreateColumnFamilies(cf_options, cf_names_except_default, &cf_handles);
    if (!s.ok()) {
      return {Status::DBOpenErr, s.ToString()};
    }

    for (auto handle : cf_handles) (*res)->DestroyColumnFamilyHandle(handle);
    (*res)->Close();
  } else {
    // We try to create column families by opening the database without column families.
    // If it's ok means we didn't create column families (cannot open without column families if created).
    // When goes wrong, we need to check whether it's caused by column families NOT being opened or not.
    // If the status message contains `Column families not opened` means that we have created the column
    // families, let's ignore the error.
    const char *not_opened_prefix = "Column families not opened";
    if (res.Msg().find(not_opened_prefix) != std::string::npos) {
      return Status::OK();
    }

    return std::move(res);
  }

  return Status::OK();
}

Status Storage::Open() {
  auto guard = WriteLockGuard();
  db_closing_ = false;

  bool cache_index_and_filter_blocks = config_->rocks_db.cache_index_and_filter_blocks;
  size_t block_cache_size = config_->rocks_db.block_cache_size * MiB;
  size_t metadata_block_cache_size = config_->rocks_db.metadata_block_cache_size * MiB;
  size_t subkey_block_cache_size = config_->rocks_db.subkey_block_cache_size * MiB;
  if (block_cache_size == 0) {
    block_cache_size = metadata_block_cache_size + subkey_block_cache_size;
  }

  rocksdb::Options options = InitRocksDBOptions();
  if (auto s = CreateColumnFamilies(options); !s.IsOK()) {
    return s.Prefixed("failed to create column families");
  }

  if (config_->rocks_db.block_cache_type == BlockCacheType::kCacheTypeLRU) {
    shared_block_cache_ = rocksdb::NewLRUCache(block_cache_size, kRocksdbLRUAutoAdjustShardBits,
                                               kRocksdbCacheStrictCapacityLimit, kRocksdbLRUBlockCacheHighPriPoolRatio);
  } else {
    rocksdb::HyperClockCacheOptions hcc_cache_options(block_cache_size, kRockdbHCCAutoAdjustCharge);
    shared_block_cache_ = hcc_cache_options.MakeSharedCache();
  }

  rocksdb::BlockBasedTableOptions metadata_table_opts = InitTableOptions();
  metadata_table_opts.block_cache = shared_block_cache_;
  metadata_table_opts.pin_l0_filter_and_index_blocks_in_cache = true;
  metadata_table_opts.cache_index_and_filter_blocks = cache_index_and_filter_blocks;
  metadata_table_opts.cache_index_and_filter_blocks_with_high_priority = true;

  rocksdb::ColumnFamilyOptions metadata_opts(options);
  metadata_opts.table_factory.reset(rocksdb::NewBlockBasedTableFactory(metadata_table_opts));
  metadata_opts.compaction_filter_factory = std::make_shared<MetadataFilterFactory>(this);
  metadata_opts.disable_auto_compactions = config_->rocks_db.disable_auto_compactions;
  // Enable whole key bloom filter in memtable
  metadata_opts.memtable_whole_key_filtering = true;
  metadata_opts.memtable_prefix_bloom_size_ratio = 0.1;
  metadata_opts.table_properties_collector_factories.emplace_back(
      NewCompactOnExpiredTableCollectorFactory(std::string(kMetadataColumnFamilyName), 0.3));
  SetBlobDB(&metadata_opts);

  rocksdb::BlockBasedTableOptions subkey_table_opts = InitTableOptions();
  subkey_table_opts.block_cache = shared_block_cache_;
  subkey_table_opts.pin_l0_filter_and_index_blocks_in_cache = true;
  subkey_table_opts.cache_index_and_filter_blocks = cache_index_and_filter_blocks;
  subkey_table_opts.cache_index_and_filter_blocks_with_high_priority = true;
  rocksdb::ColumnFamilyOptions subkey_opts(options);
  subkey_opts.table_factory.reset(rocksdb::NewBlockBasedTableFactory(subkey_table_opts));
  subkey_opts.compaction_filter_factory = std::make_shared<SubKeyFilterFactory>(this);
  subkey_opts.disable_auto_compactions = config_->rocks_db.disable_auto_compactions;
  subkey_opts.table_properties_collector_factories.emplace_back(
      NewCompactOnExpiredTableCollectorFactory(std::string(kPrimarySubkeyColumnFamilyName), 0.3));
  SetBlobDB(&subkey_opts);

  std::vector<rocksdb::ColumnFamilyDescriptor> column_families;
  // Caution: don't change the order of column family, or the handle will be mismatched
  column_families.emplace_back(rocksdb::kDefaultColumnFamilyName, subkey_opts);
  column_families.emplace_back(std::string(kMetadataColumnFamilyName), metadata_opts);

  auto start = std::chrono::high_resolution_clock::now();
  db_ = GET_OR_RET(util::DBOpen(options, config_->db_dir, column_families, &cf_handles_));
  auto end = std::chrono::high_resolution_clock::now();
  int64_t duration = std::chrono::duration_cast<std::chrono::milliseconds>(end - start).count();

  if (!db_) {
    INFO("[storage] Failed to load the data from disk: {} ms", duration);
    return {Status::DBOpenErr};
  }
  INFO("[storage] Success to load the data from disk: {} ms", duration);

  return Status::OK();
}

Status Storage::CreateBackup(uint64_t *sequence_number) {
  INFO("[storage] Start to create new backup");
  std::lock_guard<std::mutex> lg(config_->backup_mu);
  std::string task_backup_dir = config_->backup_dir;

  std::string tmpdir = task_backup_dir + ".tmp";
  // Maybe there is a dirty tmp checkpoint, try to clean it
  rocksdb::DestroyDB(tmpdir, rocksdb::Options());

  // 1) Create checkpoint of rocksdb for backup
  rocksdb::Checkpoint *checkpoint = nullptr;
  rocksdb::Status s = rocksdb::Checkpoint::Create(db_.get(), &checkpoint);
  if (!s.ok()) {
    WARN("Failed to create checkpoint object for backup. Error: {}", s.ToString());
    return {Status::NotOK, s.ToString()};
  }

  std::unique_ptr<rocksdb::Checkpoint> checkpoint_guard(checkpoint);
  s = checkpoint->CreateCheckpoint(tmpdir, config_->rocks_db.write_buffer_size * MiB, sequence_number);
  if (!s.ok()) {
    WARN("Failed to create checkpoint (snapshot) for backup. Error: {}", s.ToString());
    return {Status::DBBackupErr, s.ToString()};
  }

  // 2) Rename tmp backup to real backup dir
  if (s = rocksdb::DestroyDB(task_backup_dir, rocksdb::Options()); !s.ok()) {
    WARN("[storage] Failed to clean old backup. Error: {}", s.ToString());
    return {Status::NotOK, s.ToString()};
  }

  if (s = env_->RenameFile(tmpdir, task_backup_dir); !s.ok()) {
    WARN("[storage] Failed to rename tmp backup. Error: {}", s.ToString());
    // Just try best effort
    if (s = rocksdb::DestroyDB(tmpdir, rocksdb::Options()); !s.ok()) {
      WARN("[storage] Failed to clean tmp backup. Error: {}", s.ToString());
    }

    return {Status::NotOK, s.ToString()};
  }

  // 'backup_mu_' can guarantee 'backup_creating_time_secs_' is thread-safe
  backup_creating_time_secs_ = util::GetTimeStamp<std::chrono::seconds>();

  INFO("[storage] Success to create new backup");
  return Status::OK();
}

void Storage::PurgeOldBackups(uint32_t num_backups_to_keep, uint32_t backup_max_keep_hours) {
  auto now_secs = util::GetTimeStamp<std::chrono::seconds>();
  std::lock_guard<std::mutex> lg(config_->backup_mu);
  std::string task_backup_dir = config_->backup_dir;

  // Return if there is no backup
  auto s = env_->FileExists(task_backup_dir);
  if (!s.ok()) return;

  // No backup is needed to keep or the backup is expired, we will clean it.
  bool backup_expired =
      (backup_max_keep_hours != 0 && backup_creating_time_secs_ + backup_max_keep_hours * 3600 < now_secs);
  if (num_backups_to_keep == 0 || backup_expired) {
    s = rocksdb::DestroyDB(task_backup_dir, rocksdb::Options());
    if (s.ok()) {
      INFO("[storage] Succeeded cleaning old backup that was created at {}", backup_creating_time_secs_);
    } else {
      INFO("[storage] Failed cleaning old backup that was created at {}. Error: {}", backup_creating_time_secs_,
           s.ToString());
    }
  }
}

rocksdb::SequenceNumber Storage::LatestSeqNumber() { return db_->GetLatestSequenceNumber(); }

rocksdb::Status Storage::Get(engine::Context &ctx, const rocksdb::ReadOptions &options, const rocksdb::Slice &key,
                             std::string *value) {
  return Get(ctx, options, db_->DefaultColumnFamily(), key, value);
}

rocksdb::Status Storage::Get(engine::Context &ctx, const rocksdb::ReadOptions &options,
                             rocksdb::ColumnFamilyHandle *column_family, const rocksdb::Slice &key,
                             std::string *value) {
  if (ctx.txn_context_enabled) {
    CHECK(options.snapshot != nullptr);
    CHECK(ctx.GetSnapshot()->GetSequenceNumber() == options.snapshot->GetSequenceNumber());
  }
  rocksdb::Status s;
  if (is_txn_mode_ && txn_write_batch_->GetWriteBatch()->Count() > 0) {
    s = txn_write_batch_->GetFromBatchAndDB(db_.get(), options, column_family, key, value);
  } else if (ctx.batch && ctx.txn_context_enabled) {
    s = ctx.batch->GetFromBatchAndDB(db_.get(), options, column_family, key, value);
  } else {
    s = db_->Get(options, column_family, key, value);
  }

  recordKeyspaceStat(column_family, s);
  return s;
}

rocksdb::Status Storage::Get(engine::Context &ctx, const rocksdb::ReadOptions &options, const rocksdb::Slice &key,
                             rocksdb::PinnableSlice *value) {
  return Get(ctx, options, db_->DefaultColumnFamily(), key, value);
}

rocksdb::Status Storage::Get(engine::Context &ctx, const rocksdb::ReadOptions &options,
                             rocksdb::ColumnFamilyHandle *column_family, const rocksdb::Slice &key,
                             rocksdb::PinnableSlice *value) {
  if (ctx.txn_context_enabled) {
    CHECK(options.snapshot != nullptr);
    CHECK(ctx.GetSnapshot()->GetSequenceNumber() == options.snapshot->GetSequenceNumber());
  }
  rocksdb::Status s;
  if (is_txn_mode_ && txn_write_batch_->GetWriteBatch()->Count() > 0) {
    s = txn_write_batch_->GetFromBatchAndDB(db_.get(), options, column_family, key, value);
  } else if (ctx.txn_context_enabled && ctx.batch) {
    s = ctx.batch->GetFromBatchAndDB(db_.get(), options, column_family, key, value);
  } else {
    s = db_->Get(options, column_family, key, value);
  }

  recordKeyspaceStat(column_family, s);
  return s;
}

rocksdb::Iterator *Storage::NewIterator(engine::Context &ctx, const rocksdb::ReadOptions &options) {
  return NewIterator(ctx, options, db_->DefaultColumnFamily());
}

void Storage::recordKeyspaceStat(const rocksdb::ColumnFamilyHandle *column_family, const rocksdb::Status &s) {
  if (column_family->GetName() != kMetadataColumnFamilyName) return;

  // Don't record keyspace hits here because we cannot tell
  // if the key was expired or not. So we record it when parsing the metadata.
  if (s.IsNotFound() || s.IsInvalidArgument()) {
    RecordStat(StatType::KeyspaceMisses, 1);
  }
}

rocksdb::Iterator *Storage::NewIterator(engine::Context &ctx, const rocksdb::ReadOptions &options,
                                        rocksdb::ColumnFamilyHandle *column_family) {
  if (ctx.txn_context_enabled) {
    CHECK(options.snapshot != nullptr);
    CHECK(ctx.GetSnapshot()->GetSequenceNumber() == options.snapshot->GetSequenceNumber());
  }
  auto iter = db_->NewIterator(options, column_family);
  if (is_txn_mode_ && txn_write_batch_->GetWriteBatch()->Count() > 0) {
    return txn_write_batch_->NewIteratorWithBase(column_family, iter, &options);
  } else if (ctx.txn_context_enabled && ctx.batch && ctx.batch->GetWriteBatch()->Count() > 0) {
    return ctx.batch->NewIteratorWithBase(column_family, iter, &options);
  }
  return iter;
}

void Storage::MultiGet(engine::Context &ctx, const rocksdb::ReadOptions &options,
                       rocksdb::ColumnFamilyHandle *column_family, const size_t num_keys, const rocksdb::Slice *keys,
                       rocksdb::PinnableSlice *values, rocksdb::Status *statuses) {
  if (ctx.txn_context_enabled) {
    CHECK(options.snapshot != nullptr);
    CHECK(ctx.GetSnapshot()->GetSequenceNumber() == options.snapshot->GetSequenceNumber());
  }
  if (is_txn_mode_ && txn_write_batch_->GetWriteBatch()->Count() > 0) {
    txn_write_batch_->MultiGetFromBatchAndDB(db_.get(), options, column_family, num_keys, keys, values, statuses,
                                             false);
  } else if (ctx.txn_context_enabled && ctx.batch) {
    ctx.batch->MultiGetFromBatchAndDB(db_.get(), options, column_family, num_keys, keys, values, statuses, false);
  } else {
    db_->MultiGet(options, column_family, num_keys, keys, values, statuses, false);
  }

  for (size_t i = 0; i < num_keys; i++) {
    recordKeyspaceStat(column_family, statuses[i]);
  }
}

rocksdb::Status Storage::Write(engine::Context &ctx, const rocksdb::WriteOptions &options,
                               rocksdb::WriteBatch *updates) {
  if (is_txn_mode_) {
    // The batch won't be flushed until the transaction was committed or rollback
    return rocksdb::Status::OK();
  }
  return writeToDB(ctx, options, updates);
}

rocksdb::Status Storage::writeToDB(engine::Context &ctx, const rocksdb::WriteOptions &options,
                                   rocksdb::WriteBatch *updates) {
  // No point trying to commit an empty write batch: in fact this will fail on read-only DBs
  // even if the write batch is empty.
  if (updates->Count() == 0) {
    return rocksdb::Status::OK();
  }

  if (ctx.txn_context_enabled) {
    // Extract writes from the updates and append to the ctx.batch
    if (ctx.batch == nullptr) {
      ctx.batch = std::make_unique<rocksdb::WriteBatchWithIndex>();
    }
    WriteBatchIndexer handle(ctx);
    auto s = updates->Iterate(&handle);
    if (!s.ok()) return s;
  } else {
    CHECK(ctx.batch == nullptr);
  }

  return db_->Write(options, updates);
}

rocksdb::Status Storage::Delete(engine::Context &ctx, const rocksdb::WriteOptions &options,
                                rocksdb::ColumnFamilyHandle *cf_handle, const rocksdb::Slice &key) {
  auto batch = GetWriteBatchBase();
  auto s = batch->Delete(cf_handle, key);
  if (!s.ok()) {
    return s;
  }
  return Write(ctx, options, batch->GetWriteBatch());
}

rocksdb::Status Storage::DeleteRange(engine::Context &ctx, const rocksdb::WriteOptions &options,
                                     rocksdb::ColumnFamilyHandle *cf_handle, Slice begin, Slice end) {
  auto batch = GetWriteBatchBase();
  auto s = batch->DeleteRange(cf_handle, begin, end);
  if (!s.ok()) {
    return s;
  }

  return Write(ctx, options, batch->GetWriteBatch());
}

rocksdb::Status Storage::DeleteRange(engine::Context &ctx, Slice begin, Slice end) {
  return DeleteRange(ctx, default_write_opts_, GetCFHandle(ColumnFamilyID::Metadata), begin, end);
}

void Storage::FlushBlockCache() { shared_block_cache_->EraseUnRefEntries(); }

Status Storage::SyncWAL() {
  auto s = db_->SyncWAL();
  if (!s.ok()) {
    return {Status::NotOK, s.ToString()};
  }
  return Status::OK();
}

void Storage::RecordStat(StatType type, uint64_t v) {
  switch (type) {
    case StatType::FlushCount:
      db_stats_->flush_count.fetch_add(v, std::memory_order_relaxed);
      break;
    case StatType::CompactionCount:
      db_stats_->compaction_count.fetch_add(v, std::memory_order_relaxed);
      break;
    case StatType::KeyspaceHits:
      db_stats_->keyspace_hits.fetch_add(v, std::memory_order_relaxed);
      break;
    case StatType::KeyspaceMisses:
      db_stats_->keyspace_misses.fetch_add(v, std::memory_order_relaxed);
      break;
  }
}

rocksdb::ColumnFamilyHandle *Storage::GetCFHandle(ColumnFamilyID id) { return cf_handles_[static_cast<size_t>(id)]; }

rocksdb::Status Storage::Compact(rocksdb::ColumnFamilyHandle *cf, const Slice *begin, const Slice *end) {
  rocksdb::CompactRangeOptions compact_opts;
  // See https://github.com/facebook/rocksdb/issues/13671
  // change_level doesn't work well with level_compaction_dynamic_level_bytes
  compact_opts.change_level = !config_->rocks_db.level_compaction_dynamic_level_bytes;
  // For the manual compaction, we would like to force the bottommost level to be compacted.
  // Or it may use the trivial mode and some expired key-values were still exist in the bottommost level.
  compact_opts.bottommost_level_compaction = rocksdb::BottommostLevelCompaction::kForceOptimized;
  const auto &cf_handles = cf ? std::vector<rocksdb::ColumnFamilyHandle *>{cf} : cf_handles_;
  for (const auto &cf_handle : cf_handles) {
    rocksdb::Status s = db_->CompactRange(compact_opts, cf_handle, begin, end);
    if (!s.ok()) return s;
  }
  return rocksdb::Status::OK();
}

rocksdb::Status Storage::FlushMemTable(rocksdb::ColumnFamilyHandle *cf_handle, const rocksdb::FlushOptions &options) {
  const auto &cf_handles = cf_handle ? std::vector<rocksdb::ColumnFamilyHandle *>{cf_handle} : cf_handles_;
  return db_->Flush(options, cf_handles);
}

uint64_t Storage::GetTotalSize(const std::string &ns) {
  if (ns == kDefaultNamespace) {
    return sst_file_manager_->GetTotalSize();
  }

  auto begin_key = ComposeNamespaceKey(ns, "", false);
  auto end_key = util::StringNext(begin_key);

  redis::Database db(this, ns);
  uint64_t size = 0, total_size = 0;
  rocksdb::DB::SizeApproximationFlags include_both =
      rocksdb::DB::SizeApproximationFlags::INCLUDE_FILES | rocksdb::DB::SizeApproximationFlags::INCLUDE_MEMTABLES;

  for (auto cf_handle : cf_handles_) {
    rocksdb::Range r(begin_key, end_key);
    db_->GetApproximateSizes(cf_handle, &r, 1, &size, include_both);
    total_size += size;
  }

  return total_size;
}

void Storage::SetSstFileDeleteRateBytesPerSecond(int64_t delete_rate) {
  sst_file_manager_->SetDeleteRateBytesPerSecond(delete_rate);
}

void Storage::CheckDBSizeLimit() {
  bool limit_reached = false;
  if (config_->max_db_size > 0) {
    limit_reached = GetTotalSize() >= config_->max_db_size * GiB;
  }

  if (db_size_limit_reached_ == limit_reached) {
    return;
  }

  db_size_limit_reached_ = limit_reached;
  if (db_size_limit_reached_) {
    WARN("[storage] ENABLE db_size limit {} GB. Switch xrockscache to read-only mode.", config_->max_db_size);
  } else {
    WARN("[storage] DISABLE db_size limit. Switch xrockscache to read-write mode.");
  }
}

void Storage::SetIORateLimit(int64_t max_io_mb) {
  if (max_io_mb == 0) {
    max_io_mb = kIORateLimitMaxMb;
  }
  rate_limiter_->SetBytesPerSecond(max_io_mb * static_cast<int64_t>(MiB));
}

rocksdb::DB *Storage::GetDB() { return db_.get(); }

Status Storage::BeginTxn() {
  if (is_txn_mode_) {
    return Status{Status::NotOK, "cannot begin a new transaction while already in transaction mode"};
  }
  // The EXEC command is exclusive and shouldn't have multi transaction at the same time,
  // so it's fine to reset the global write batch without any lock.
  is_txn_mode_ = true;
  // Set overwrite_key to false to avoid overwriting the existing key in case
  // like downstream would parse the replication log etc.
  txn_write_batch_ = std::make_unique<rocksdb::WriteBatchWithIndex>(
      /*backup_index_comparator=*/rocksdb::BytewiseComparator(),
      /*reserved_bytes=*/0, /*overwrite_key=*/false, /*max_bytes=*/GetWriteBatchMaxBytes());
  return Status::OK();
}

Status Storage::CommitTxn() {
  if (!is_txn_mode_) {
    return Status{Status::NotOK, "cannot commit while not in transaction mode"};
  }
  engine::Context ctx(this);
  auto s = writeToDB(ctx, default_write_opts_, txn_write_batch_->GetWriteBatch());

  is_txn_mode_ = false;
  txn_write_batch_ = nullptr;
  if (s.ok()) {
    return Status::OK();
  }
  return {Status::NotOK, s.ToString()};
}

ObserverOrUniquePtr<rocksdb::WriteBatchBase> Storage::GetWriteBatchBase() {
  if (is_txn_mode_) {
    return ObserverOrUniquePtr<rocksdb::WriteBatchBase>(txn_write_batch_.get(), ObserverOrUnique::Observer);
  }
  return ObserverOrUniquePtr<rocksdb::WriteBatchBase>(
      new rocksdb::WriteBatch(0 /*reserved_bytes*/, GetWriteBatchMaxBytes()), ObserverOrUnique::Unique);
}

std::shared_lock<std::shared_mutex> Storage::ReadLockGuard() { return std::shared_lock(db_rw_lock_); }

std::unique_lock<std::shared_mutex> Storage::WriteLockGuard() { return std::unique_lock(db_rw_lock_); }

[[nodiscard]] rocksdb::ReadOptions Context::GetReadOptions() {
  rocksdb::ReadOptions read_options;
  if (txn_context_enabled) read_options.snapshot = GetSnapshot();
  return read_options;
}

[[nodiscard]] rocksdb::ReadOptions Context::DefaultScanOptions() {
  rocksdb::ReadOptions read_options = storage->DefaultScanOptions();
  if (txn_context_enabled) read_options.snapshot = GetSnapshot();
  return read_options;
}

[[nodiscard]] rocksdb::ReadOptions Context::DefaultMultiGetOptions() {
  rocksdb::ReadOptions read_options = storage->DefaultMultiGetOptions();
  if (txn_context_enabled) read_options.snapshot = GetSnapshot();
  return read_options;
}

void Context::RefreshLatestSnapshot() {
  if (snapshot_) {
    storage->GetDB()->ReleaseSnapshot(snapshot_);
  }
  snapshot_ = storage->GetDB()->GetSnapshot();
  if (batch) {
    batch->Clear();
  }
}

}  // namespace engine
