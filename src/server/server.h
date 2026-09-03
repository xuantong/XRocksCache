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

#include <inttypes.h>
#include <tbb/concurrent_vector.h>

#include <array>
#include <atomic>
#include <condition_variable>
#include <cstddef>
#include <cstdint>
#include <list>
#include <map>
#include <memory>
#include <mutex>
#include <set>
#include <shared_mutex>
#include <string>
#include <type_traits>
#include <unordered_map>
#include <utility>
#include <variant>
#include <vector>

#include "commands/commander.h"
#include "common/time_util.h"
#include "memory_profiler.h"
#include "namespace.h"
#include "server/redis_connection.h"
#include "stats/log_collector.h"
#include "stats/stats.h"
#include "storage/redis_metadata.h"
#include "storage/storage.h"
#include "task_runner.h"
#include "tls_util.h"
#include "worker.h"

constexpr const char *REDIS_VERSION = "7.0.0";

struct DBScanInfo {
  // Last scan system clock in seconds
  int64_t last_scan_time_secs = 0;
  KeyNumStats key_num_stats;
  bool is_scanning = false;
};

struct ConnContext {
  Worker *owner;
  int fd;

  ConnContext(Worker *w, int fd) : owner(w), fd(fd) {}

  bool operator<(const ConnContext &c) const {
    if (owner == c.owner) {
      return fd < c.fd;
    }

    return owner < c.owner;
  }

  bool operator==(const ConnContext &c) const { return owner == c.owner && fd == c.fd; }
};

struct ChannelSubscribeNum {
  std::string channel;
  size_t subscribe_num;
};

// CURSOR_DICT_SIZE must be 2^n where n <= 16
constexpr const size_t CURSOR_DICT_SIZE = 1024 * 16;
static_assert((CURSOR_DICT_SIZE & (CURSOR_DICT_SIZE - 1)) == 0, "CURSOR_DICT_SIZE must be 2^n");
static_assert(CURSOR_DICT_SIZE <= (1 << 16), "CURSOR_DICT_SIZE must be less than or equal to 2^16");

enum class CursorType : uint8_t {
  kTypeNone = 0,  // none
  kTypeBase = 1,  // cursor for SCAN
  kTypeHash = 2,  // cursor for HSCAN
  kTypeSet = 3,   // cursor for SSCAN
  kTypeZSet = 4,  // cursor for ZSCAN
};

enum class PauseMode {
  kOff = 0,
  kAll = 1,
  kWrite = 2,
};
struct CursorDictElement;

class NumberCursor {
 public:
  NumberCursor() = default;
  explicit NumberCursor(CursorType cursor_type, uint16_t counter, const std::string &key_name);
  explicit NumberCursor(uint64_t number_cursor) : cursor_(number_cursor) {}
  size_t GetIndex() const { return cursor_ % CURSOR_DICT_SIZE; }
  bool IsMatch(const CursorDictElement &element, CursorType cursor_type) const;
  std::string ToString() const { return std::to_string(cursor_); }

 private:
  CursorType getCursorType() const { return static_cast<CursorType>(cursor_ >> 61); }
  uint64_t cursor_;
};

struct CursorDictElement {
  NumberCursor cursor;
  std::string key_name;
};

enum SlowLog {
  kSlowLogMaxArgc = 32,
  kSlowLogMaxString = 128,
};

enum ClientType {
  kTypeNormal = (1ULL << 0),  // normal client
  kTypePubsub = (1ULL << 1),  // pubsub client
  kTypeMaster = (1ULL << 2),  // master client
  kTypeSlave = (1ULL << 3),   // slave client
};

enum class AuthResult {
  IS_USER,
  IS_ADMIN,
  INVALID_PASSWORD,
  NO_REQUIRE_PASS,
};

class SlotImport;
class SlotMigrator;

class Server {
 public:
  explicit Server(engine::Storage *storage, Config *config);
  ~Server();

  Server(const Server &) = delete;
  Server &operator=(const Server &) = delete;

  Status Start();
  void Stop();
  void Join();
  bool IsStopped() const { return stop_; }
  bool IsLoading() const { return is_loading_; }
  Config *GetConfig() { return config_; }
  static StatusOr<std::unique_ptr<redis::Commander>> LookupAndCreateCommand(const std::string &cmd_name);
  void AdjustOpenFilesLimit();
  void AdjustWorkerThreads();

  bool IsSlave() const { return false; }
  void FeedMonitorConns(redis::Connection *conn, const std::vector<std::string> &tokens);
  static std::vector<std::string> RedactSensitiveTokens(const std::vector<std::string> &tokens);
  int GetFetchFileThreadNum() const { return 0; }

  int PublishMessage(const std::string &channel, const std::string &msg);
  void SubscribeChannel(const std::string &channel, redis::Connection *conn);
  void UnsubscribeChannel(const std::string &channel, redis::Connection *conn);
  void GetChannelsByPattern(const std::string &pattern, std::vector<std::string> *channels);
  void ListChannelSubscribeNum(const std::vector<std::string> &channels,
                               std::vector<ChannelSubscribeNum> *channel_subscribe_nums);
  void PSubscribeChannel(const std::string &pattern, redis::Connection *conn);
  void PUnsubscribeChannel(const std::string &pattern, redis::Connection *conn);
  size_t GetPubSubPatternSize() const { return pubsub_patterns_.size(); }
  void SSubscribeChannel(const std::string &channel, redis::Connection *conn, uint16_t slot);
  void SUnsubscribeChannel(const std::string &channel, redis::Connection *conn, uint16_t slot);
  void GetSChannelsByPattern(const std::string &pattern, std::vector<std::string> *channels);
  void ListSChannelSubscribeNum(const std::vector<std::string> &channels,
                                std::vector<ChannelSubscribeNum> *channel_subscribe_nums);

  void BlockOnKey(const std::string &key, redis::Connection *conn);
  void UnblockOnKey(const std::string &key, redis::Connection *conn);

  // WAIT command infrastructure
  void BlockOnWait(redis::Connection *conn, rocksdb::SequenceNumber target_seq, uint64_t num_replicas);
  void WakeupWaitConnections(rocksdb::SequenceNumber seq);
  void CleanupWaitConnection(redis::Connection *conn);
  void WakeupWaitConnection(redis::Connection *conn, rocksdb::SequenceNumber seq);

  // Helper methods for WAIT command
  size_t GetReplicasReachedSequence(rocksdb::SequenceNumber target_seq);
  // Return the largest wait_context.target_seq that can wakeup given the seq.
  // If no wait_context can wakeup, return 0.
  rocksdb::SequenceNumber LargestTargetSeqToWakeup(rocksdb::SequenceNumber seq);

  std::string GetLastRandomKeyCursor();
  void SetLastRandomKeyCursor(const std::string &cursor);

  static int64_t GetCachedUnixTime();
  int64_t GetLastBgsaveTime();
  std::string GetRoleInfo();

  // An INFO entry holds its value with its original type in a variant, so each output format can
  // render it appropriately: the text format (ToString) emits the Redis-compatible representation
  // (e.g. a bool as 0/1, numbers via std::to_string) while FORMAT JSON emits the native JSON type
  // (a bool as true/false, numbers unquoted). The type is captured here at construction.
  struct InfoEntry {
    using Value = std::variant<std::string, int64_t, double, bool>;
    std::string name;
    Value val;

    InfoEntry(std::string name, std::string val) : name(std::move(name)), val(std::move(val)) {}
    InfoEntry(std::string name, std::string_view val) : name(std::move(name)), val(std::string(val)) {}
    InfoEntry(std::string name, const char *val) : name(std::move(name)), val(std::string(val)) {}
    InfoEntry(std::string name, bool v) : name(std::move(name)), val(v) {}
    // Floating-point values (incl. float, which widens to double) are stored as double.
    InfoEntry(std::string name, double v) : name(std::move(name)), val(v) {}
    // Integers (bool handled above) are stored as int64_t.
    template <typename T, std::enable_if_t<std::is_integral_v<T> && !std::is_same_v<T, bool>, int> = 0>
    InfoEntry(std::string name, T v) : name(std::move(name)), val(static_cast<int64_t>(v)) {}

    // Redis-compatible text form: strings verbatim, booleans as 0/1, numbers via std::to_string.
    std::string ValueToString() const {
      return std::visit(
          [](const auto &v) -> std::string {
            using T = std::decay_t<decltype(v)>;
            if constexpr (std::is_same_v<T, std::string>) {
              return v;
            } else if constexpr (std::is_same_v<T, bool>) {
              return v ? "1" : "0";
            } else {
              return std::to_string(v);
            }
          },
          val);
    }
  };
  using InfoEntries = std::vector<InfoEntry>;

  InfoEntries GetStatsInfo();
  InfoEntries GetServerInfo();
  InfoEntries GetMemoryInfo();
  InfoEntries GetRocksDBInfo();
  InfoEntries GetClientsInfo();
  InfoEntries GetReplicationInfo();
  InfoEntries GetCommandsStatsInfo();
  InfoEntries GetClusterInfo();
  InfoEntries GetPersistenceInfo();
  InfoEntries GetCpuInfo();
  InfoEntries GetKeyspaceInfo(const std::string &ns);

  enum class InfoFormat { Text, Json };
  std::string GetInfo(const std::string &ns, const std::vector<std::string> &sections,
                      InfoFormat format = InfoFormat::Text);
  std::string GetRocksDBStatsJson() const;

  Status AsyncCompactDB(const std::string &begin_key = "", const std::string &end_key = "");
  Status AsyncBgSaveDB();
  Status AsyncPurgeOldBackups(uint32_t num_backups_to_keep, uint32_t backup_max_keep_hours);
  Status AsyncScanDBSize(const std::string &ns);
  void GetLatestKeyNumStats(const std::string &ns, KeyNumStats *stats);
  int64_t GetLastScanTime(const std::string &ns);

  std::string GenerateCursorFromKeyName(const std::string &key_name, CursorType cursor_type, const char *prefix = "");
  std::string GetKeyNameFromCursor(const std::string &cursor, CursorType cursor_type);

  int DecrClientNum();
  int IncrClientNum();
  int IncrMonitorClientNum();
  int DecrMonitorClientNum();
  int IncrBlockedClientNum();
  int DecrBlockedClientNum();
  std::string GetClientsStr(const redis::Connection *conn);
  uint64_t GetClientID();
  void KillClient(int64_t *killed, const std::string &addr, uint64_t id, uint64_t type, bool skipme,
                  redis::Connection *conn);

  LogCollector<PerfEntry> *GetPerfLog() { return &perf_log_; }
  LogCollector<SlowEntry> *GetSlowLog() { return &slow_log_; }
  void SlowlogPushEntryIfNeeded(const std::vector<std::string> *args, uint64_t duration, const redis::Connection *conn);

  std::shared_lock<std::shared_mutex> WorkConcurrencyGuard();
  std::unique_lock<std::shared_mutex> WorkExclusivityGuard();

  // CLIENT PAUSE / CLIENT UNPAUSE
  void PauseConns(uint64_t end_time_ms, PauseMode mode);
  // Returns true if the connection was suspended (caller must stop processing further commands).
  bool PauseConnIfNeeded(redis::Connection *conn, const std::string &cmd_name, uint64_t cmd_flags);
  void UnpauseConns();
  void RemovePausedConn(redis::Connection *conn);

  Stats stats;
  engine::Storage *storage;
  MemoryProfiler memory_profiler;
  static inline std::atomic<int64_t> unix_time_secs = 0;

  void UpdateWatchedKeysFromArgs(const std::vector<std::string> &args, const redis::CommandAttributes &attr);
  void UpdateWatchedKeysManually(const std::vector<std::string> &keys);
  void WatchKey(redis::Connection *conn, const std::vector<std::string> &keys);
  static bool IsWatchedKeysModified(redis::Connection *conn);
  void ResetWatchedKeys(redis::Connection *conn);
  Namespace *GetNamespace() { return &namespace_; }

  AuthResult AuthenticateUser(const std::string &user_password, std::string *ns);

#ifdef ENABLE_OPENSSL
  UniqueSSLContext ssl_ctx;
#endif

 private:
  void cron();
  void recordInstantaneousMetrics();
  static void updateCachedTime();
  void updateWatchedKeysFromRange(const std::vector<std::string> &args, const redis::CommandKeyRange &range);
  void updateAllWatchedKeys();
  void increaseWorkerThreads(size_t delta);
  void decreaseWorkerThreads(size_t delta);
  void cleanupExitedWorkerThreads(bool force);
  // Helper function to clean up wait contexts for a given connection
  // It would not hold the wait_contexts_mu_ and the caller should hold it.
  void cleanupWaitConnection(redis::Connection *conn);

  std::atomic<bool> stop_ = false;
  std::atomic<bool> is_loading_ = false;
  int64_t start_time_secs_;
  Config *config_ = nullptr;
  std::string last_random_key_cursor_;
  std::mutex last_random_key_cursor_mu_;

  // client counters
  std::atomic<uint64_t> client_id_{1};
  std::atomic<int> connected_clients_{0};
  std::atomic<int> monitor_clients_{0};
  std::atomic<uint64_t> total_clients_{0};

  // namespace
  Namespace namespace_;

  // Some jobs to operate DB should be unique
  std::mutex db_job_mu_;
  bool db_compacting_ = false;
  bool is_bgsave_in_progress_ = false;
  int64_t last_bgsave_timestamp_secs_ = -1;
  std::string last_bgsave_status_ = "ok";
  int64_t last_bgsave_duration_secs_ = -1;

  std::map<std::string, DBScanInfo> db_scan_infos_;

  LogCollector<SlowEntry> slow_log_;
  LogCollector<PerfEntry> perf_log_;

  std::map<std::string, std::list<ConnContext>> pubsub_channels_;
  std::map<std::string, std::list<ConnContext>> pubsub_patterns_;
  std::mutex pubsub_channels_mu_;
  std::vector<std::map<std::string, std::list<ConnContext>>> pubsub_shard_channels_;
  std::mutex pubsub_shard_channels_mu_;
  std::map<std::string, std::list<ConnContext>> blocking_keys_;
  std::mutex blocking_keys_mu_;

  std::atomic<int> blocked_clients_{0};

  // WAIT command blocking infrastructure
  struct WaitContext {
    redis::Connection *conn;
    rocksdb::SequenceNumber target_seq;
    uint64_t num_replicas;

    WaitContext(redis::Connection *c, rocksdb::SequenceNumber seq, uint64_t replicas)
        : conn(c), target_seq(seq), num_replicas(replicas) {}
  };
  std::multimap<rocksdb::SequenceNumber, WaitContext> wait_contexts_;
  std::shared_mutex wait_contexts_mu_;

  // threads
  std::shared_mutex works_concurrency_rw_lock_;
  std::thread cron_thread_;
  std::thread compaction_checker_thread_;
  TaskRunner task_runner_;
  std::vector<std::unique_ptr<WorkerThread>> worker_threads_;
  tbb::concurrent_queue<std::unique_ptr<WorkerThread>> recycle_worker_threads_;

  // memory
  std::atomic<int64_t> memory_startup_use_ = 0;

  // transaction
  std::atomic<size_t> watched_key_size_ = 0;
  std::map<std::string, std::set<redis::Connection *>> watched_key_map_;
  std::shared_mutex watched_key_mutex_;

  // SCAN ring buffer
  std::atomic<uint16_t> cursor_counter_ = {0};
  using CursorDictType = std::array<CursorDictElement, CURSOR_DICT_SIZE>;
  std::unique_ptr<CursorDictType> cursor_dict_;

  // Conn pause state (CLIENT PAUSE)
  std::atomic<uint64_t> conn_pause_end_time_{0};
  std::atomic<PauseMode> conn_pause_mode_{PauseMode::kOff};
  std::mutex conn_pause_mu_;
  // Fields are captured while the connection is alive; UnpauseConns never
  // dereferences the pointer after releasing the lock, preventing use-after-free.
  struct PausedConnEntry {
    Worker *worker;
    int fd;
    uint64_t id;
  };
  std::vector<PausedConnEntry> paused_conns_;
};
