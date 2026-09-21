package local.xrc;

import io.lettuce.core.*;
import io.lettuce.core.api.StatefulRedisConnection;
import io.lettuce.core.codec.StringCodec;
import io.lettuce.core.output.NestedMultiOutput;
import io.lettuce.core.output.StatusOutput;
import io.lettuce.core.protocol.CommandArgs;
import io.lettuce.core.protocol.CommandType;
import io.lettuce.core.protocol.ProtocolVersion;
import org.junit.jupiter.api.*;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.SpringBootConfiguration;
import org.springframework.boot.autoconfigure.EnableAutoConfiguration;
import org.springframework.boot.autoconfigure.data.redis.LettuceClientConfigurationBuilderCustomizer;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.context.annotation.Bean;
import org.springframework.data.redis.connection.RedisConnectionFactory;
import org.springframework.data.redis.core.*;
import org.springframework.data.redis.serializer.RedisSerializer;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.*;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(classes = RedisCompatibilityTest.App.class, properties = {
    "spring.data.redis.host=127.0.0.1", "spring.data.redis.port=${XRC_JAVA_PORT:6692}",
    "spring.data.redis.password=${XRC_JAVA_PASSWORD}", "spring.data.redis.timeout=10s",
    "spring.data.redis.database=0", "spring.data.redis.repositories.enabled=false"})
class RedisCompatibilityTest {
    @SpringBootConfiguration @EnableAutoConfiguration
    static class App {
        @Bean LettuceClientConfigurationBuilderCustomizer resp2() {
            return builder -> builder.clientOptions(ClientOptions.builder().protocolVersion(ProtocolVersion.RESP2).build());
        }
        @Bean RedisTemplate<String, byte[]> bytes(RedisConnectionFactory factory) {
            RedisTemplate<String, byte[]> t = new RedisTemplate<>();
            t.setConnectionFactory(factory);
            t.setKeySerializer(RedisSerializer.string());
            t.setValueSerializer(RedisSerializer.byteArray());
            t.afterPropertiesSet();
            return t;
        }
    }
    @Autowired StringRedisTemplate strings;
    @Autowired RedisTemplate<String, byte[]> bytes;
    private final String prefix = "java-it:" + UUID.randomUUID() + ":";
    private final Set<String> keys = ConcurrentHashMap.newKeySet();
    String key(String suffix) { String k = prefix + suffix; keys.add(k); return k; }
    RedisClient client(boolean password) {
        RedisURI.Builder uri = RedisURI.Builder.redis("127.0.0.1", Integer.parseInt(System.getenv().getOrDefault("XRC_JAVA_PORT", "6692")))
            .withTimeout(Duration.ofSeconds(10));
        if (password) uri.withPassword(Objects.requireNonNull(System.getenv("XRC_JAVA_PASSWORD")).toCharArray());
        RedisClient c = RedisClient.create(uri.build());
        c.setOptions(ClientOptions.builder().protocolVersion(ProtocolVersion.RESP2).build());
        return c;
    }
    @AfterEach void cleanup() { if (!keys.isEmpty()) strings.delete(keys); }

    @Test void springStringsAndBatch() {
        String a = key("a"), b = key("b"), missing = key("missing");
        assertNull(strings.opsForValue().get(missing));
        strings.opsForValue().set(a, "中文\u0000value");
        assertEquals("中文\u0000value", strings.opsForValue().get(a));
        strings.opsForValue().multiSet(Map.of(a, "A", b, "B"));
        assertEquals(Arrays.asList("A", null, "B"), strings.opsForValue().multiGet(List.of(a, missing, b)));
        assertEquals(2L, strings.countExistingKeys(List.of(a, b, missing)));
        assertEquals(2L, strings.delete(List.of(a, b)));
        assertFalse(strings.hasKey(a));
    }
    @Test void springConditionalWritesAndExpiry() throws Exception {
        String k = key("ttl");
        assertTrue(strings.opsForValue().setIfAbsent(k, "a", Duration.ofSeconds(60)));
        assertFalse(strings.opsForValue().setIfAbsent(k, "b", Duration.ofSeconds(60)));
        assertTrue(strings.opsForValue().setIfPresent(k, "c", Duration.ofSeconds(60)));
        assertFalse(strings.opsForValue().setIfPresent(key("missing"), "c"));
        assertTrue(strings.expire(k, 30, TimeUnit.SECONDS));
        assertTrue(strings.getExpire(k) > 0 && strings.getExpire(k) <= 30);
        assertTrue(strings.expire(k, Duration.ofMillis(150)));
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5);
        while (strings.hasKey(k) && System.nanoTime() < deadline) Thread.sleep(25);
        assertNull(strings.opsForValue().get(k));
        assertEquals(-2L, strings.getExpire(k, TimeUnit.MILLISECONDS));
        assertFalse(strings.expire(k, Duration.ofSeconds(1)));
    }
    @Test void binaryValuesAndSizeBoundaries() {
        for (int size : new int[]{0, 1, 1024*1024, 5*1024*1024}) {
            byte[] value = new byte[size]; new Random(size).nextBytes(value);
            String k = key("binary:" + size);
            bytes.opsForValue().set(k, value);
            assertArrayEquals(value, bytes.opsForValue().get(k));
        }
        String maxKey = "k".repeat(512*1024); keys.add(maxKey);
        bytes.opsForValue().set(maxKey, new byte[]{1});
        assertArrayEquals(new byte[]{1}, bytes.opsForValue().get(maxKey));
        assertThrows(RuntimeException.class, () -> bytes.opsForValue().set(maxKey + "x", new byte[]{1}));
        assertThrows(RuntimeException.class, () -> bytes.opsForValue().set(key("oversized"), new byte[5*1024*1024+1]));
        assertEquals("PONG", strings.execute((RedisCallback<String>) c -> c.ping()));
    }
    @Test void countersAndConcurrentAccess() throws Exception {
        String k = key("counter");
        assertEquals(1L, strings.opsForValue().increment(k));
        assertEquals(0L, strings.opsForValue().decrement(k));
        assertEquals(12L, strings.opsForValue().increment(k, 12));
        assertEquals(10L, strings.opsForValue().decrement(k, 2));
        ExecutorService pool = Executors.newFixedThreadPool(4);
        try {
            List<Callable<Void>> jobs = new ArrayList<>();
            for (int n = 0; n < 4; n++) jobs.add(() -> { for (int i=0;i<25;i++) strings.opsForValue().increment(k); return null; });
            for (Future<Void> f : pool.invokeAll(jobs, 30, TimeUnit.SECONDS)) f.get();
            assertEquals("110", strings.opsForValue().get(k));
        } finally { pool.shutdownNow(); }
        strings.opsForValue().set(k, "not-a-number");
        assertThrows(RuntimeException.class, () -> strings.opsForValue().increment(k));
        strings.opsForValue().set(k, Long.toString(Long.MAX_VALUE));
        assertThrows(RuntimeException.class, () -> strings.opsForValue().increment(k));
        assertEquals(Long.toString(Long.MAX_VALUE), strings.opsForValue().get(k));
    }
    @Test void lettuceConnectionAndAdministrativeCommands() {
        RedisClient client = client(true);
        try (StatefulRedisConnection<String,String> connection = client.connect()) {
            var c = connection.sync();
            assertEquals("PONG", c.ping());
            assertEquals("message", c.dispatch(CommandType.PING, new StatusOutput<>(StringCodec.UTF8), new CommandArgs<>(StringCodec.UTF8).add("message")));
            assertEquals("echo", c.echo("echo"));
            assertEquals("OK", c.auth(System.getenv("XRC_JAVA_PASSWORD")));
            assertEquals("OK", c.auth("default", System.getenv("XRC_JAVA_PASSWORD")));
            var hello = c.dispatch(CommandType.HELLO, new NestedMultiOutput<>(StringCodec.UTF8), new CommandArgs<>(StringCodec.UTF8).add(2));
            assertTrue(hello.contains("xrockscache"));
            assertEquals("OK", c.select(0));
            assertThrows(RedisCommandExecutionException.class, () -> c.select(1));
            assertTrue(c.info().contains("max_value_bytes:5242880"));
            assertTrue(c.dbsize() >= 0);
            assertTrue(c.command().isEmpty());
            assertEquals("OK", c.clientSetname("java-it"));
            assertEquals("OK", c.dispatch(CommandType.CLIENT, new StatusOutput<>(StringCodec.UTF8),
                new CommandArgs<>(StringCodec.UTF8).add("SETINFO").add("LIB-NAME").add("xrc-java-it")));
            // 当前 CLIENT 为握手兼容占位，不保存名称或分配真实 ID。
            assertNull(c.clientGetname());
            assertEquals(0L, c.clientId());
            assertEquals("OK", c.quit());
        } finally { client.shutdown(); }
    }
    @Test void lettuceSetOptionsAndTTLContract() {
        RedisClient client = client(true);
        try (var connection = client.connect()) {
            var c = connection.sync(); String k = key("options");
            assertEquals("OK", c.set(k, "a", SetArgs.Builder.ex(60)));
            assertNull(c.set(k, "ignored", SetArgs.Builder.nx()));
            assertEquals("OK", c.set(k, "b", SetArgs.Builder.xx().keepttl()));
            assertTrue(c.ttl(k) > 0 && c.ttl(k) <= 60);
            assertEquals("b", c.setGet(k, "c", SetArgs.Builder.px(60000)));
            assertTrue(c.pttl(k) > 0 && c.pttl(k) <= 60000);
            assertTrue(c.expire(k, 60)); assertTrue(c.pexpire(k, 60000));
            assertTrue(c.expire(k, 0)); assertEquals(-2L, c.ttl(k));
            c.set(k, "default-ttl");
            assertTrue(c.ttl(k) > 1295900 && c.ttl(k) <= 1296000);
            assertThrows(RedisCommandExecutionException.class, () -> c.set(k, "bad", SetArgs.Builder.ex(1296001)));
            assertEquals("default-ttl", c.get(k));
            String[] many = new String[65]; Arrays.fill(many, k);
            assertThrows(RedisCommandExecutionException.class, () -> c.mget(many));
            assertEquals("PONG", c.ping());
        } finally { client.shutdown(); }
    }
    @Test void wrongAuthenticationIsRejected() {
        RedisClient client = client(true);
        try (var connection = client.connect()) {
            var c = connection.sync();
            assertThrows(RedisCommandExecutionException.class, () -> c.auth("incorrect-password"));
            assertThrows(RedisCommandExecutionException.class, c::ping);
            assertEquals("OK", c.auth(System.getenv("XRC_JAVA_PASSWORD")));
            assertEquals("PONG", c.ping());
        } finally { client.shutdown(); }
    }
    @Test void batchLimitsAndAtomicFailure() {
        String k = key("batch-large"), untouched = key("untouched");
        byte[] value = new byte[5*1024*1024];
        bytes.opsForValue().set(k, value);
        assertThrows(RuntimeException.class, () -> bytes.opsForValue().multiGet(List.of(k,k,k,k)));
        Map<String,byte[]> tooLarge = new LinkedHashMap<>();
        tooLarge.put(untouched, value); tooLarge.put(key("second"), value);
        assertThrows(RuntimeException.class, () -> bytes.opsForValue().multiSet(tooLarge));
        assertNull(bytes.opsForValue().get(untouched));
        Map<String,byte[]> invalidKey = new LinkedHashMap<>();
        invalidKey.put(untouched, new byte[]{1}); invalidKey.put("k".repeat(512*1024+1), new byte[]{2});
        assertThrows(RuntimeException.class, () -> bytes.opsForValue().multiSet(invalidKey));
        assertNull(bytes.opsForValue().get(untouched));
        assertArrayEquals(value, bytes.opsForValue().get(k));
    }
    @Test void springPipeline() {
        String k = key("pipeline");
        var results = strings.executePipelined((RedisCallback<Object>) c -> {
            c.stringCommands().set(k.getBytes(java.nio.charset.StandardCharsets.UTF_8), new byte[]{65});
            c.stringCommands().get(k.getBytes(java.nio.charset.StandardCharsets.UTF_8));
            return null;
        });
        assertEquals(2, results.size()); assertEquals("A", results.get(1));
    }
}
