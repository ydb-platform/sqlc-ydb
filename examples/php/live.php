<?php

declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use YdbPlatform\Ydb\Session;
use YdbPlatform\Ydb\Table;
use YdbPlatform\Ydb\Ydb;

function check(bool $condition, string $message): void
{
    if (!$condition) {
        throw new RuntimeException($message);
    }
}

function source(string $relative): string
{
    $content = file_get_contents(dirname(__DIR__) . '/' . $relative);
    if ($content === false) {
        throw new RuntimeException('Cannot read ' . $relative);
    }
    return $content;
}

function scheme(Table $table, string $sql): void
{
    $table->retrySession(static function (Session $session) use ($sql): void {
        $session->schemeQuery($sql);
    }, true);
}

function dropTables(Table $table, array $names): void
{
    foreach ($names as $name) {
        scheme($table, 'DROP TABLE ' . $name . ';');
    }
}

/** @return list<string> */
function createTables(Table $table, string $sql, array $names): array
{
    $statements = array_values(array_filter(array_map('trim', explode(';', $sql))));
    if (count($statements) !== count($names)) {
        throw new RuntimeException('Schema statement count does not match its table list');
    }

    $created = [];
    try {
        foreach ($statements as $index => $statement) {
            scheme($table, $statement . ';');
            $created[] = $names[$index];
        }
    } catch (Throwable $error) {
        dropTables($table, array_reverse($created));
        throw $error;
    }

    return array_reverse($created);
}

function runAuthors(Table $table): void
{
    $createdTables = createTables($table, source('authors/schema.sql'), ['authors']);
    try {
        $queries = new Authors\Native\Queries($table);
        $id = '18446744073709551615';
        check($queries->getAuthor($id) === null, 'authors: missing :one row is not null');
        $created = $queries->createAuthor(new Authors\Native\CreateAuthorParams(
            authorId: $id,
            authorName: 'Ada',
            biography: null,
        ));
        check($created?->id === $id && $created->name === 'Ada' && $created->bio === null, 'authors: create/decode failed');
        check($queries->getAuthorName($id)?->name === 'Ada', 'authors: scalar row failed');
        check($queries->listAuthors()[0]->id === $id, 'authors: :many failed');
        $queries->upsertAuthor(new Authors\Native\UpsertAuthorParams(
            authorId: $id,
            authorName: 'Ada Lovelace',
            biography: 'programmer',
        ));
        check($queries->getAuthor($id)?->bio === 'programmer', 'authors: upsert failed');
        $queries->deleteAuthor($id);
        check($queries->getAuthor($id) === null, 'authors: delete failed');
    } finally {
        dropTables($table, $createdTables);
    }
}

function runBatch(Table $table): void
{
    $createdTables = createTables($table, source('batch/schema.sql'), ['authors', 'books']);
    try {
        $queries = new Batch\Native\Queries($table);
        $authorId = '18446744073709551615';
        $bookId = '18446744073709551614';
        $author = $queries->createAuthor(new Batch\Native\CreateAuthorParams(
            authorId: $authorId,
            name: 'Octavia',
            biography: '{"born":1947}',
        ));
        check($author?->authorId === $authorId && $author->biography === '{"born":1947}', 'batch: author Json failed');
        $available = 1788957296789123;
        $book = $queries->createBook(new Batch\Native\CreateBookParams(
            bookId: $bookId,
            authorId: $authorId,
            isbn: '978-0',
            bookType: 'novel',
            title: 'Kindred',
            year: 1979,
            available: $available,
            tags: '["history","science-fiction"]',
        ));
        check($book?->bookId === $bookId && $book->available === $available && $book->tags === '["history","science-fiction"]', 'batch: Uint64/Timestamp/Json decode failed');
        check($queries->booksByYear(1979)[0]->authorId === $authorId, 'batch: filtered :many failed');
        $queries->updateBook(new Batch\Native\UpdateBookParams(
            title: 'Kindred (updated)',
            tags: '{"shelf":"read"}',
            bookId: $bookId,
        ));
        check($queries->getBiography($authorId)?->biography === '{"born":1947}', 'batch: Optional<Json> decode failed');
        $queries->deleteBook($bookId);
        $queries->deleteBookExecResult($bookId);
        $queries->deleteBookNamedFunc($bookId);
        $queries->deleteBookNamedSign($bookId);
        check($queries->getAuthor($authorId)?->authorId === $authorId, 'batch: author read failed');
    } finally {
        dropTables($table, $createdTables);
    }
}

function runBooktest(Table $table): void
{
    $createdTables = createTables($table, source('booktest/schema.sql'), ['authors', 'books']);
    try {
        $queries = new Booktest\Native\Queries($table);
        $authorId = '91';
        $bookId = '92';
        $queries->createAuthor(new Booktest\Native\CreateAuthorParams(
            authorId: $authorId,
            name: 'Ursula',
        ));
        $available = 1735787045678123;
        $queries->createBook(new Booktest\Native\CreateBookParams(
            bookId: $bookId,
            authorId: $authorId,
            isbn: 'isbn',
            bookType: 'novel',
            title: 'Earthsea',
            publicationYear: 1968,
            available: $available,
            tags: '["fantasy"]',
        ));
        check($queries->getAuthor($authorId)?->name === 'Ursula', 'booktest: author read failed');
        check($queries->getBook($bookId)?->available === $available, 'booktest: Timestamp lost microseconds');
        check($queries->booksByTitleYear(new Booktest\Native\BooksByTitleYearParams(
            title: 'Earthsea',
            publicationYear: 1968,
        ))[0]->bookId === $bookId, 'booktest: compound parameters failed');
        check($queries->booksByTags('["fantasy"]')[0]->name === 'Ursula', 'booktest: LEFT JOIN/Json failed');
        check($queries->sayHello('YDB')?->greeting === 'hello YDB', 'booktest: scalar expression failed');
        $queries->updateBook(new Booktest\Native\UpdateBookParams(
            title: 'A Wizard of Earthsea',
            tags: '["classic"]',
            bookId: $bookId,
        ));
        $queries->updateBookIsbn(new Booktest\Native\UpdateBookIsbnParams(
            title: 'A Wizard of Earthsea',
            tags: '["classic"]',
            isbn: 'new-isbn',
            bookId: $bookId,
        ));
        $queries->deleteAuthorBeforeYear(new Booktest\Native\DeleteAuthorBeforeYearParams(
            publicationYear: 1900,
            authorId: $authorId,
        ));
        $queries->deleteBook($bookId);
        check($queries->getBook($bookId) === null, 'booktest: delete failed');
    } finally {
        dropTables($table, $createdTables);
    }
}

function runJets(Table $table): void
{
    $createdTables = createTables($table, source('jets/schema.sql'), ['pilots', 'jets', 'languages', 'pilot_languages']);
    try {
        $queries = new Jets\Native\Queries($table);
        check($queries->countPilots()?->pilotCount === '0', 'jets: COUNT must decode as Uint64 decimal text');
        $table->retrySession(static function (Session $session): void {
            $session->query('UPSERT INTO pilots (id, name) VALUES (1, "Amelia"u), (2, "Bessie"u);');
            $session->commit();
        }, false);
        $pilots = $queries->listPilots();
        check(count($pilots) === 2 && $pilots[0]->id === 1 && $pilots[1]->name === 'Bessie', 'jets: list failed');
        $queries->deletePilot(1);
        check($queries->countPilots()?->pilotCount === '1', 'jets: delete/count failed');
    } finally {
        dropTables($table, $createdTables);
    }
}

function runOndeck(Table $table): void
{
    $createdTables = [];
    try {
        scheme($table, source('ondeck/schema/0001_city.sql'));
        $createdTables[] = 'city';
        scheme($table, source('ondeck/schema/0002_venue.sql'));
        $createdTables[] = 'venues';
        scheme($table, source('ondeck/schema/0003_rename_venue.sql'));
        $createdTables[array_key_last($createdTables)] = 'venue';
        scheme($table, source('ondeck/schema/0004_add_created_at.sql'));
        scheme($table, source('ondeck/schema/0005_drop_column.sql'));

        $queries = new Ondeck\Native\Queries($table);
        $city = $queries->createCity(new Ondeck\Native\CreateCityParams(
            name: 'London',
            slug: 'london',
        ));
        check($city?->slug === 'london' && $queries->getCity('london')?->name === 'London', 'ondeck: city create/read failed');
        $queries->updateCityName(new Ondeck\Native\UpdateCityNameParams(
            name: 'Greater London',
            slug: 'london',
        ));
        check($queries->listCities()[0]->name === 'Greater London', 'ondeck: city update/list failed');
        $createdAt = 1788948672345123;
        $venue = $queries->createVenue(new Ondeck\Native\CreateVenueParams(
            id: '7',
            slug: 'roundhouse',
            name: 'Roundhouse',
            city: 'london',
            createdAt: $createdAt,
            spotifyPlaylist: 'spotify:playlist:1',
            status: 'open',
            statuses: '["open"]',
            tags: '{"genre":"rock"}',
        ));
        check($venue?->id === '7', 'ondeck: venue create failed');
        $loaded = $queries->getVenue(new Ondeck\Native\GetVenueParams(
            slug: 'roundhouse',
            city: 'london',
        ));
        check($loaded?->createdAt === $createdAt && $loaded->statuses === '["open"]' && $loaded->tags === '{"genre":"rock"}', 'ondeck: optional exact values failed');
        check($queries->listVenues('london')[0]->id === '7', 'ondeck: venue list failed');
        check($queries->venueCountByCity()[0]->venueCount === '1', 'ondeck: grouped COUNT failed');
        check($queries->updateVenueName(new Ondeck\Native\UpdateVenueNameParams(
            name: 'The Roundhouse',
            slug: 'roundhouse',
        ))?->id === '7', 'ondeck: update RETURNING failed');
        $queries->deleteVenue('roundhouse');
        check($queries->getVenue(new Ondeck\Native\GetVenueParams(
            slug: 'roundhouse',
            city: 'london',
        )) === null, 'ondeck: venue delete failed');
    } finally {
        dropTables($table, array_reverse($createdTables));
    }
}

$dsn = getenv('SQLC_YDB_TEST_DSN');
if ($dsn === false || $dsn === '') {
    throw new RuntimeException('SQLC_YDB_TEST_DSN is required, for example grpc://localhost:2136/local');
}
$parts = parse_url($dsn);
if (!is_array($parts) || !isset($parts['host'], $parts['port'], $parts['path'])) {
    throw new InvalidArgumentException('SQLC_YDB_TEST_DSN must contain scheme, host, port and database path');
}
$ydb = new Ydb([
    'database' => $parts['path'],
    'endpoint' => $parts['host'] . ':' . $parts['port'],
    'discovery' => false,
    'iam_config' => [
        'anonymous' => true,
        'insecure' => ($parts['scheme'] ?? 'grpc') === 'grpc',
    ],
]);
$table = $ydb->table();

runAuthors($table);
runBatch($table);
runBooktest($table);
runJets($table);
runOndeck($table);

echo "All PHP generated-query examples passed.\n";
