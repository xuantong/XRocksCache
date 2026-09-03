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
#include <cstdint>
#include <string>

#include "fmt/format.h"

// crc16
constexpr const uint16_t HASH_SLOTS_MASK = 0x3fff;
constexpr const uint16_t HASH_SLOTS_SIZE = HASH_SLOTS_MASK + 1;  // 16384
constexpr const uint16_t HASH_SLOTS_MAX_ITERATIONS = 50;
constexpr const int kClusterSlots = HASH_SLOTS_SIZE;

inline constexpr const char *errInvalidSlotID = "Invalid slot id";
inline constexpr const char *errSlotOutOfRange = "Slot is out of range";
inline constexpr const char *errSlotRangeInvalid = "Slot range is invalid";

/// SlotRange is a lightweight range of Redis hash slots covering [start, end].
struct SlotRange {
  SlotRange(int start, int end) : start(start), end(end) {}
  SlotRange() : start(-1), end(-1) {}
  bool IsValid() const { return start >= 0 && end >= 0 && start <= end && end < kClusterSlots; }

  bool Contains(int slot) const { return IsValid() && slot >= start && slot <= end; }

  bool HasOverlap(const SlotRange &rhs) const {
    return IsValid() && rhs.IsValid() && end >= rhs.start && rhs.end >= start;
  }

  std::string String() const {
    if (!IsValid()) return "-1";
    if (start == end) return fmt::format("{}", start);
    return fmt::format("{}-{}", start, end);
  }

  static SlotRange GetPoint(int slot) { return {slot, slot}; }

  bool operator==(const SlotRange &rhs) const { return start == rhs.start && end == rhs.end; }
  bool operator!=(const SlotRange &rhs) const { return !(*this == rhs); }

  int start;
  int end;
};

uint16_t Crc16(const char *buf, size_t len);
uint16_t GetSlotIdFromKey(std::string_view key);
std::string_view GetTagFromKey(std::string_view key);
