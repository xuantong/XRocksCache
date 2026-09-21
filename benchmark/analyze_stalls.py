import json, re, pathlib, sys, statistics
p = pathlib.Path(sys.argv[1])
text = '\n'.join(f.read_text(errors='replace') for f in p.glob('LOG*'))
causes = {}
for line in text.splitlines():
    if 'Stopping writes because' in line or 'Stalling writes because' in line:
        reason = 'memtable' if 'immutable memtables' in line else 'L0' if 'level-0 files' in line else 'pending_compaction'
        causes[reason] = causes.get(reason, 0) + 1
starts, flushes = {}, []
for line in text.splitlines():
    if 'EVENT_LOG_v1 ' not in line: continue
    try: e = json.loads(line.split('EVENT_LOG_v1 ', 1)[1])
    except ValueError: continue
    if e.get('event') == 'flush_started': starts[e['job']] = e
    if e.get('event') == 'flush_finished' and e['job'] in starts:
        s = starts[e['job']]; seconds = (e['time_micros'] - s['time_micros']) / 1e6
        if seconds > 0: flushes.append((seconds, s['total_data_size']))
timeline = (p / 'timeline.log').read_text(errors='replace')
metrics = {}
for name in ['rocksdb_immutable_memtables','rocksdb_l0_files','rocksdb_pending_compaction_bytes','rocksdb_background_errors']:
    vals = [int(v) for v in re.findall(r'^'+name+r':(\d+)', timeline, re.M)]
    metrics[name + '_max'] = max(vals) if vals else None
rss = [int(v) for v in re.findall(r'^VmRSS:\s+(\d+)', timeline, re.M)]
write = [int(v) for v in re.findall(r'^write_bytes:\s+(\d+)', timeline, re.M)]
result = {'stall_log_events': causes, 'metrics': metrics, 'sampled_peak_rss_kib': max(rss) if rss else None, 'process_write_bytes_last': write[-1] if write else None, 'flush_count':len(flushes)}
if flushes:
    result.update(flush_seconds_mean=statistics.mean(x[0] for x in flushes), flush_seconds_max=max(x[0] for x in flushes), flush_aggregate_mib_sec=sum(x[1] for x in flushes)/sum(x[0] for x in flushes)/1048576)
output=json.dumps(result,indent=2)
(p/'diagnosis.json').write_text(output)
print(output)
