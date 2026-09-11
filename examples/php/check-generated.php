<?php

declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use Ydb\Type;
use Ydb\Type\PrimitiveTypeId;
use Ydb\Value;
use YdbPlatform\Ydb\Iam;
use YdbPlatform\Ydb\Logger\NullLogger;
use YdbPlatform\Ydb\Retry\RetryParams;
use YdbPlatform\Ydb\Session;
use YdbPlatform\Ydb\Table;
use YdbPlatform\Ydb\Ydb as YdbClient;

function check(bool $condition, string $message): void
{
    if (!$condition) {
        throw new RuntimeException($message);
    }
}

$namespaces = [
    'Authors\\Native',
    'Batch\\Native',
    'Booktest\\Native',
    'Jets\\Native',
    'Ondeck\\Native',
];

foreach ($namespaces as $namespace) {
    check(class_exists($namespace . '\\Queries'), $namespace . '\\Queries was not generated or autoloaded');
    check(class_exists($namespace . '\\YdbRawExecutor'), $namespace . '\\YdbRawExecutor was not generated or autoloaded');
    check(class_exists($namespace . '\\YdbValueCodec'), $namespace . '\\YdbValueCodec was not generated or autoloaded');
}

require dirname(__DIR__, 2) . '/internal/endtoend/testdata/php_codec/expected/php/YdbRuntime.php';
$codec = 'PhpCodec\\YdbValueCodec';
foreach (['typedFloat', 'typedOptionalFloat'] as $bindFloat) {
    foreach ([1e40, -1e40] as $overflow) {
        try {
            $codec::$bindFloat($overflow, 'float_overflow');
            throw new RuntimeException($bindFloat . ' accepted a value that becomes infinity in Float32');
        } catch (RangeException) {
        }
    }
    foreach ([0.0, 3.4028234663852886e38, -3.4028234663852886e38] as $finite) {
        $value = $codec::$bindFloat($finite, 'float_boundary');
        check(is_finite($value->getValue()->getFloatValue()), 'Float32 boundary became infinity');
    }
}
check($codec::typedDouble(1e40, 'double')->getValue()->getDoubleValue() === 1e40, 'Float32 limit leaked into Double');

$maximumUint64 = '18446744073709551615';
$typedUint64 = $codec::typedUint64($maximumUint64, 'maximum');
check($typedUint64->getType()->getTypeId() === PrimitiveTypeId::UINT64, 'Uint64 binding has the wrong YDB type');
$serializedUint64 = $typedUint64->serializeToString();
$roundTrippedUint64 = new \Ydb\TypedValue();
$roundTrippedUint64->mergeFromString($serializedUint64);
check($codec::uint64($roundTrippedUint64->getValue(), 'maximum') === $maximumUint64, 'Uint64 protobuf round-trip lost the full unsigned range');

foreach ([0, 4291747199999999] as $timestamp) {
    $typedTimestamp = $codec::typedTimestamp($timestamp, 'timestamp');
    check($typedTimestamp->getType()->getTypeId() === PrimitiveTypeId::TIMESTAMP, 'Timestamp binding has the wrong YDB type');
    $serializedTimestamp = $typedTimestamp->serializeToString();
    $roundTrippedTimestamp = new \Ydb\TypedValue();
    $roundTrippedTimestamp->mergeFromString($serializedTimestamp);
    check($codec::timestamp($roundTrippedTimestamp->getValue(), 'timestamp') === $timestamp, 'Timestamp protobuf round-trip lost microseconds');
}
$optionalTimestamp = $codec::typedOptionalTimestamp(4291747199999999, 'optional_timestamp');
$serializedOptionalTimestamp = $optionalTimestamp->serializeToString();
$roundTrippedOptionalTimestamp = new \Ydb\TypedValue();
$roundTrippedOptionalTimestamp->mergeFromString($serializedOptionalTimestamp);
check($codec::optionalTimestamp($roundTrippedOptionalTimestamp->getValue(), 'optional_timestamp') === 4291747199999999, 'Optional<Timestamp> protobuf round-trip lost the maximum value');
foreach (['typedTimestamp', 'typedOptionalTimestamp'] as $timestampBinder) {
    try {
        $codec::$timestampBinder(4291747200000000, 'beyond_timestamp');
        throw new RuntimeException($timestampBinder . ' accepted the first out-of-range YDB Timestamp');
    } catch (RangeException $error) {
        check(str_contains($error->getMessage(), 'Timestamp range'), $timestampBinder . ' range diagnostic is unclear');
    }
}
$beyondTimestamp = new Value(['uint64_value' => '4291747200000000']);
try {
    $codec::timestamp($beyondTimestamp, 'beyond_timestamp_result');
    throw new RuntimeException('Timestamp decoder accepted the first out-of-range value');
} catch (UnexpectedValueException $error) {
    check(str_contains($error->getMessage(), 'Timestamp range'), 'Timestamp decode range diagnostic is unclear');
}

$json = '{"identifier":9007199254740993,"nested":[true,null,"text"]}';
$typedJson = $codec::typedJson($json, 'json');
check($typedJson->getType()->getTypeId() === PrimitiveTypeId::JSON, 'Json binding has the wrong YDB type');
check($codec::json($typedJson->getValue(), 'json') === $json, 'Json binding changed the source text');

$optionalJson = $codec::typedOptionalJson(null, 'optional_json');
check($optionalJson->getType()->getType() === 'optional_type', 'Optional<Json> binding is not optional');
check($optionalJson->getValue()->getValue() === 'null_flag_value', 'Optional<Json> null has the wrong protobuf case');
check($codec::optionalJson($optionalJson->getValue(), 'optional_json') === null, 'Optional<Json> null did not decode as null');

$codec::assertType(new Type(['type_id' => PrimitiveTypeId::UINT64]), PrimitiveTypeId::UINT64, false, 'uint64_column');
try {
    $codec::uint64(new Value(['text_value' => '1']), 'wrong_case');
    throw new RuntimeException('wrong protobuf value case was accepted');
} catch (UnexpectedValueException $error) {
    check(str_contains($error->getMessage(), 'uint64_value'), 'wrong-case diagnostic lacks the expected case');
}
try {
    $codec::typedJson('{broken', 'bad_json');
    throw new RuntimeException('invalid JSON was accepted');
} catch (InvalidArgumentException $error) {
    check(str_contains($error->getMessage(), 'valid JSON'), 'invalid-JSON diagnostic is unclear');
}
try {
    $codec::typedUint64('18446744073709551616', 'too_large');
    throw new RuntimeException('out-of-range Uint64 was accepted');
} catch (RangeException $error) {
    check(str_contains($error->getMessage(), 'Uint64'), 'Uint64 range diagnostic is unclear');
}

check(class_exists('Batch\\Native\\CreateBookParams'), 'typed parameter DTO was not generated');
check(class_exists('Booktest\\Native\\BooksByTagsRow'), 'LEFT JOIN row DTO was not generated');
check(class_exists('Jets\\Native\\CountPilotsRow'), 'COUNT row DTO was not generated');
check(class_exists('Ondeck\\Native\\CreateVenueParams'), 'Optional Timestamp/Json DTO was not generated');

final class RetryProbeCredentials extends Iam
{
    public function __construct()
    {
    }

    public function token($force = false): string
    {
        return 'test-token';
    }
}

final class RetryProbeYdb extends YdbClient
{
    public function __construct()
    {
    }

    public function needDiscovery(): bool
    {
        return false;
    }

    public function endpoint(): string
    {
        return 'reset-client';
    }

    public function grpcOpts(): array
    {
        return [];
    }

    public function getGrpcTimeout(): ?int
    {
        return null;
    }
}

final class RetryProbeCall
{
    public function __construct(private readonly string $kind, private readonly string $sessionId)
    {
    }

    public function wait(): array
    {
        if ($this->kind === 'first') {
            check($this->sessionId === 'session-0', 'first client received a query for the wrong session');
            return [null, (object) ['code' => 14, 'details' => 'injected retryable failure']];
        }
        if ($this->kind === 'second') {
            check($this->sessionId === 'session-1', 'second client received a query for the wrong session');
            $result = new \Ydb\Table\ExecuteQueryResult(['result_sets' => [new \Ydb\ResultSet()]]);
            $packedResult = new \Google\Protobuf\Any();
            $packedResult->pack($result);
            $operation = new \Ydb\Operations\Operation([
                'ready' => true,
                'status' => \Ydb\StatusIds\StatusCode::SUCCESS,
                'result' => $packedResult,
            ]);
            return [new \Ydb\Table\ExecuteDataQueryResponse(['operation' => $operation]), (object) ['code' => 0]];
        }
        ++RetryProbeClient::$resetExecutions;
        throw new RuntimeException('client reset inside a failed executor leaked into the next retry attempt');
    }
}

final class RetryProbeClient
{
    public static int $resetExecutions = 0;
    public int $executions = 0;
    public string $sql = '';
    public bool $keepInCache = false;

    public function __construct(private readonly string $kind, array $options = [])
    {
    }

    public function ExecuteDataQuery(\Ydb\Table\ExecuteDataQueryRequest $request, array $metadata, array $options): RetryProbeCall
    {
        ++$this->executions;
        $this->sql = $request->getQuery()->getYqlText();
        $this->keepInCache = $request->getQueryCachePolicy()->getKeepInCache();
        return new RetryProbeCall($this->kind, $request->getSessionId());
    }
}

final class RetryProbeTable extends Table
{
    /** @var list<RetryProbeClient> */
    private array $clients;
    private int $attempt = 0;
    private RetryProbeCredentials $probeCredentials;
    private RetryProbeYdb $probeYdb;
    /** @var list<string> */
    public array $taken = [];
    /** @var list<string> */
    public array $released = [];

    public function __construct()
    {
        $this->clients = [new RetryProbeClient('first'), new RetryProbeClient('second')];
        $this->probeCredentials = new RetryProbeCredentials();
        $this->probeYdb = new RetryProbeYdb();
    }

    public function client(): RetryProbeClient
    {
        return $this->clients[$this->attempt];
    }

    public function meta(): array
    {
        return [];
    }

    public function credentials(): Iam
    {
        return $this->probeCredentials;
    }

    public function ydb(): YdbClient
    {
        return $this->probeYdb;
    }

    public function path(): string
    {
        return '/local';
    }

    public function getLogger(): NullLogger
    {
        return new NullLogger();
    }

    public function sessionTaken($session): void
    {
        $this->taken[] = $session->id();
    }

    public function sessionReleased($session): void
    {
        $this->released[] = $session->id();
    }

    public function retrySession(Closure $userFunc, bool $idempotent = false, RetryParams $params = null)
    {
        $failure = null;
        for ($attempt = 0; $attempt < 2; ++$attempt) {
            $this->attempt = $attempt;
            try {
                return $userFunc(new Session($this, 'session-' . $attempt));
            } catch (Throwable $error) {
                $failure = $error;
            }
        }
        throw $failure;
    }

    /** @return list<RetryProbeClient> */
    public function clients(): array
    {
        return $this->clients;
    }
}

$retryProbeTable = new RetryProbeTable();
(new Authors\Native\Queries($retryProbeTable))->deleteAuthor('1');
$retryProbeClients = $retryProbeTable->clients();
check(!str_contains($retryProbeClients[0]->sql, 'DECLARE '), 'generated SQL still contains a parameter declaration');
check(str_contains($retryProbeClients[0]->sql, 'DELETE FROM authors WHERE id = $author_id;'), 'inline SQL was not passed to the SDK');
check($retryProbeClients[0]->sql === $retryProbeClients[1]->sql, 'retry changed the inline SQL');
check($retryProbeClients[0]->executions === 1, 'first retry attempt did not use the first Table client');
check($retryProbeClients[1]->executions === 1, 'second retry attempt did not rebuild the executor from the current Table client');
check(RetryProbeClient::$resetExecutions === 0, 'failed executor client was reused by a later retry attempt');
check($retryProbeTable->taken === ['session-0', 'session-1'], 'retry sessions were not taken exactly once');
check($retryProbeTable->released === ['session-0', 'session-1'], 'retry sessions were not released after success and failure');

foreach (['authors', 'batch', 'booktest', 'jets', 'ondeck'] as $family) {
    foreach (glob(dirname(__DIR__) . '/' . $family . '/php/native/*.php') ?: [] as $file) {
        $command = escapeshellarg(PHP_BINARY) . ' -l ' . escapeshellarg($file);
        $output = [];
        exec($command, $output, $status);
        check($status === 0, 'PHP syntax check failed for ' . $file . "\n" . implode("\n", $output));
    }
}


check($retryProbeClients[1]->keepInCache, 'parameterized query did not request plan caching');

$queries = new Authors\Native\Queries($retryProbeTable);
$decode = new ReflectionMethod($queries, 'decodeRows');
$result = new \Ydb\Table\ExecuteQueryResult([
    'result_sets' => [new \Ydb\ResultSet(['truncated' => true])],
]);
try {
    $decode->invoke($queries, $result, 'ListAuthors', [], static fn($items) => null);
    throw new RuntimeException('truncated result was accepted as complete');
} catch (UnexpectedValueException $error) {
    check(str_contains($error->getMessage(), 'truncated'), 'unexpected truncation error');
}

echo "Imported and checked generated PHP for all five examples against YDB PHP SDK 1.16.1.\n";
