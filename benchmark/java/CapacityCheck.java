import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.util.*;

/** 独立 RESP2 容量检查；只连接调用者指定的隔离实例。 */
public class CapacityCheck {
    static class Client implements AutoCloseable {
        final Socket socket = new Socket();
        final InputStream in;
        final OutputStream out;
        Client(int port) throws IOException {
            socket.connect(new InetSocketAddress("127.0.0.1", port), 5000);
            socket.setSoTimeout(30000);
            in = new BufferedInputStream(socket.getInputStream());
            out = new BufferedOutputStream(socket.getOutputStream());
        }
        String line() throws IOException {
            ByteArrayOutputStream b = new ByteArrayOutputStream();
            int c;
            while ((c = in.read()) != -1 && c != '\r') b.write(c);
            if (c == -1 || in.read() != '\n') throw new EOFException();
            return b.toString(StandardCharsets.UTF_8);
        }
        byte[] call(byte[]... args) throws IOException {
            out.write(bytes("*" + args.length + "\r\n"));
            for (byte[] a : args) {
                out.write(bytes("$" + a.length + "\r\n")); out.write(a); out.write(bytes("\r\n"));
            }
            out.flush();
            int type = in.read(); String head = line();
            if (type == '-') throw new IOException(head);
            if (type == '$') {
                int n = Integer.parseInt(head);
                if (n == -1) return null;
                if (n < 0 || n > 16 * 1024 * 1024) throw new IOException("invalid response size");
                byte[] value = in.readNBytes(n);
                if (value.length != n || in.read() != '\r' || in.read() != '\n') throw new EOFException();
                return value;
            }
            if (type != '+' && type != ':') throw new IOException("unexpected response type");
            return bytes(head);
        }
        public void close() throws IOException { socket.close(); }
    }
    static byte[] bytes(String s) { return s.getBytes(StandardCharsets.UTF_8); }
    static byte[] key(long i) { return bytes("java-capacity:" + i); }
    static byte[] value(long i, int size) {
        byte[] value = new byte[size]; new Random(0x4a524f434bL ^ i).nextBytes(value); return value;
    }
    public static void main(String[] args) throws Exception {
        int port = Integer.parseInt(args[0]);
        long target = Long.parseLong(args[1]);
        File manifest = new File(args[2]);
        boolean verifyOnly = args.length > 3 && args[3].equals("verify");
        boolean latencyOnly = args.length > 3 && args[3].equals("latency");
        long started = System.nanoTime(), total = 0, count = 0, stalls = 0, waitMillis = 0;
        try (Client c = new Client(port)) {
            if (!Arrays.equals(c.call(bytes("PING")), bytes("PONG"))) throw new IOException("PING failed");
            if (!verifyOnly && !latencyOnly) {
                Random sizes = new Random(20260917);
                try (PrintWriter samples = new PrintWriter(new FileWriter(manifest))) {
                    long report = 0;
                    while (total < target) {
                        int size = 65536 + sizes.nextInt(1048576 - 65536 + 1);
                        byte[] expected = value(count, size);
                        long deadline = System.nanoTime() + 120_000_000_000L;
                        while (true) {
                            try {
                                if (!Arrays.equals(c.call(bytes("SET"), key(count), expected), bytes("OK"))) throw new IOException("SET failed");
                                break;
                            } catch (IOException e) {
                                if (!e.getMessage().contains("Write stall") || System.nanoTime() >= deadline) throw e;
                                stalls++; waitMillis += 100; Thread.sleep(100);
                            }
                        }
                        if (count % 100 == 0 || total + size >= target) {
                            if (!Arrays.equals(expected, c.call(bytes("GET"), key(count)))) throw new IOException("value mismatch " + count);
                            samples.println(count + " " + size); samples.flush();
                        }
                        total += size; count++;
                        if (total - report >= 1024L * 1024 * 1024) {
                            byte[] info = c.call(bytes("INFO"));
                            String metrics = new String(info, StandardCharsets.UTF_8).replace('\r', ' ').replace('\n', ' ');
                            System.out.printf(Locale.ROOT, "loaded_keys=%d logical_bytes=%d elapsed_seconds=%.2f write_stalls=%d retry_wait_ms=%d metrics=%s%n", count, total, (System.nanoTime()-started)/1e9, stalls, waitMillis, metrics.replaceAll(".*(rocksdb_stall_micros:[^ ]+.*rocksdb_num_running_compactions:[^ ]+.*rocksdb_num_running_flushes:[^ ]+.*rocksdb_memtable_bytes:[^ ]+.*rocksdb_l0_files:[^ ]+).*", "$1"));
                            report = total;
                        }
                    }
                }
                System.out.printf("LOAD_PASS keys=%d logical_bytes=%d target_bytes=%d%n", count, total, target);
            }
            long checked = 0;
            if (latencyOnly) {
                ArrayList<Long> micros = new ArrayList<>();
                long errors = 0;
                try (Scanner samples = new Scanner(manifest)) {
                    while (samples.hasNextLong()) {
                        long id = samples.nextLong(); int size = samples.nextInt();
                        byte[] expected = value(id, size);
                        long t0 = System.nanoTime();
                        try {
                            byte[] got = c.call(bytes("GET"), key(id));
                            long elapsed = (System.nanoTime() - t0) / 1000;
                            if (!Arrays.equals(expected, got)) throw new IOException("sample mismatch " + id);
                            micros.add(elapsed);
                        } catch (IOException e) {
                            errors++;
                            System.err.println("READ_FAILED completed=" + micros.size() + " errors=" + errors + " error=" + e);
                            throw e;
                        }
                    }
                }
                if (micros.isEmpty()) throw new IOException("no successful latency samples");
                Collections.sort(micros);
                long sum = 0; for (long v : micros) sum += v;
                System.out.printf(Locale.ROOT, "READ_RT samples=%d errors=%d avg_us=%.1f p50_us=%d p95_us=%d p99_us=%d max_us=%d%n",
                    micros.size(), errors, (double) sum / micros.size(), percentile(micros, .50), percentile(micros, .95), percentile(micros, .99), micros.get(micros.size()-1));
                return;
            }
            try (Scanner samples = new Scanner(manifest)) {
                while (samples.hasNextLong()) {
                    long id = samples.nextLong(); int size = samples.nextInt();
                    if (!Arrays.equals(value(id, size), c.call(bytes("GET"), key(id)))) throw new IOException("sample mismatch " + id);
                    long ttl = Long.parseLong(new String(c.call(bytes("TTL"), key(id)), StandardCharsets.UTF_8));
                    if (ttl <= 0 || ttl > 1296000) throw new IOException("invalid TTL " + ttl);
                    checked++;
                }
            }
            if (checked == 0) throw new IOException("no samples");
            System.out.println("VERIFY_PASS samples=" + checked);
            System.out.println(new String(c.call(bytes("INFO")), StandardCharsets.UTF_8));
        }
    }
    static long percentile(ArrayList<Long> sorted, double p) {
        int index = (int)Math.ceil(p * sorted.size()) - 1;
        return sorted.get(Math.max(0, Math.min(index, sorted.size() - 1)));
    }
}
