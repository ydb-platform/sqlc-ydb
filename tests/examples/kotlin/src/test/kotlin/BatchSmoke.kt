import java.sql.DriverManager
import java.time.Instant
import org.jetbrains.exposed.v1.core.DatabaseConfig
import org.jetbrains.exposed.v1.jdbc.Database
import tech.ydb.common.transaction.TxMode
import tech.ydb.core.grpc.GrpcTransport
import tech.ydb.query.QueryClient
import tech.ydb.query.tools.SessionRetryContext
import tech.ydb.exposed.dialect.registerYdbDialect
import tech.ydb.exposed.dialect.ydbTransaction

private const val SCHEMA = "CREATE TABLE books (book_id Uint64 NOT NULL, author_id Uint64 NOT NULL, isbn Utf8 NOT NULL, book_type Utf8 NOT NULL, title Utf8 NOT NULL, year Int32 NOT NULL, available Timestamp NOT NULL, tags Json NOT NULL, PRIMARY KEY(book_id));"
private class ExpectedRollback : RuntimeException()

/** Run against a disposable database without a books table. */
fun main() {
    val endpoint = requireNotNull(System.getenv("YDB_CONNECTION_STRING"))
    DriverManager.getConnection("jdbc:ydb:$endpoint").use { connection ->
        connection.createStatement().use { it.execute(SCHEMA) }
        try {
            val jdbc = batch.jdbc.Queries(connection)
            jdbc.createBooks(emptyList())
            check(jdbc.booksByYear(2026).isEmpty())
            jdbc.createBooks(listOf(
                batch.jdbc.CreateBooksBooksItem(-1L, 42, "isbn", "paper", "First", 2026, Instant.EPOCH, "[1,true]"),
                batch.jdbc.CreateBooksBooksItem(7, 42, "isbn2", "paper", "Second", 2026, Instant.EPOCH, "[]")))
            check(jdbc.booksByYear(2026).size == 2)
            check(jdbc.booksByYear(2026).any { it.bookId == -1L && it.tags == "[1,true]" })
            connection.autoCommit = false
            jdbc.createBooks(listOf(batch.jdbc.CreateBooksBooksItem(8, 42, "rollback", "paper", "Rollback", 2027, Instant.EPOCH, "[]")))
            connection.rollback()
            connection.autoCommit = true
            check(jdbc.booksByYear(2027).isEmpty())
            GrpcTransport.forConnectionString(endpoint).build().use { transport ->
                QueryClient.newClient(transport).build().use { client ->
                    val retry = SessionRetryContext.create(client).build()
                    val native = batch.nativeapi.Queries(retry)
                    native.createBooks(emptyList())
                    native.createBooks(listOf(batch.nativeapi.CreateBooksBooksItem(9, 42, "native", "paper", "Native", 2028, Instant.EPOCH, "[false]")))
                    check(jdbc.booksByYear(2028).single().tags == "[false]")
                    retry.supplyStatus { session ->
                        val transaction = session.createNewTransaction(TxMode.SERIALIZABLE_RW)
                        batch.nativeapi.Queries(transaction).createBooks(listOf(batch.nativeapi.CreateBooksBooksItem(10, 42, "rollback", "paper", "Rollback", 2029, Instant.EPOCH, "[]")))
                        transaction.rollback()
                    }.join().expectSuccess()
                    check(jdbc.booksByYear(2029).isEmpty())
                }
            }
            registerYdbDialect()
            val database = Database.connect("jdbc:ydb:$endpoint", driver = "tech.ydb.jdbc.YdbDriver", databaseConfig = DatabaseConfig { useNestedTransactions = false })
            ydbTransaction(database) {
                val exposed = batch.exposed.Queries(this)
                exposed.createBooks(emptyList())
                exposed.createBooks(listOf(batch.exposed.CreateBooksBooksItem(11, 42, "exposed", "paper", "Exposed", 2030, Instant.EPOCH, "[true]")))
                check(jdbc.booksByYear(2030).isEmpty())
            }
            check(jdbc.booksByYear(2030).single().tags == "[true]")
            try {
                ydbTransaction(database) {
                    batch.exposed.Queries(this).createBooks(listOf(batch.exposed.CreateBooksBooksItem(12, 42, "rollback", "paper", "Rollback", 2031, Instant.EPOCH, "[]")))
                    throw ExpectedRollback()
                }
            } catch (_: ExpectedRollback) { }
            check(jdbc.booksByYear(2031).isEmpty())
            println("Kotlin native/JDBC/Exposed batch smoke passed")
        } finally {
            if (!connection.autoCommit) { connection.rollback(); connection.autoCommit = true }
            connection.createStatement().use { it.execute("DROP TABLE books;") }
        }
    }
}
