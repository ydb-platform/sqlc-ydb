package authors.smoke

import authors.nativeapi.Queries as NativeQueries
import authors.jdbc.Queries as JdbcQueries
import authors.exposed.Queries as ExposedQueries
import java.nio.file.Files
import java.nio.file.Path
import java.sql.Connection
import java.sql.DriverManager
import org.jetbrains.exposed.v1.core.DatabaseConfig
import org.jetbrains.exposed.v1.jdbc.Database
import tech.ydb.common.transaction.TxMode
import tech.ydb.core.grpc.GrpcTransport
import tech.ydb.query.QueryClient
import tech.ydb.query.tools.QueryReader
import tech.ydb.query.tools.SessionRetryContext
import tech.ydb.exposed.dialect.registerYdbDialect
import tech.ydb.exposed.dialect.ydbTransaction

private const val MAX_UINT64 = -1L
private const val SECOND_ID = 7L
private const val ROLLBACK_ID = 8L

/** Run from examples/authors against a disposable database without an authors table. */
fun main() {
    val endpoint = requireNotNull(System.getenv("YDB_CONNECTION_STRING")) {
        "YDB_CONNECTION_STRING is required"
    }
    require(endpoint.isNotBlank()) { "YDB_CONNECTION_STRING is required" }
    val schema = Files.readString(Path.of("schema.sql"))
    require(schema.isNotBlank()) { "schema.sql must not be empty" }
    runNative(endpoint, schema)
    runJdbc(endpoint, schema)
    runExposed(endpoint, schema)
}

private fun runNative(endpoint: String, schema: String) {
    GrpcTransport.forConnectionString(endpoint).build().use { transport ->
        QueryClient.newClient(transport).build().use { client ->
            val retry = SessionRetryContext.create(client).build()
            fun ddl(sql: String) {
                retry.supplyResult { session ->
                    QueryReader.readFrom(session.createQuery(sql, TxMode.NONE))
                }.join().getValue()
            }
            ddl(schema)
            try {
                val queries = NativeQueries(retry)
                check(queries.getAuthor(MAX_UINT64) == null)
                val created = requireNotNull(queries.createAuthor(MAX_UINT64, "Unsigned", null))
                check(created.id == MAX_UINT64 && created.name == "Unsigned" && created.bio == null)
                check(queries.getAuthor(MAX_UINT64)?.bio == null)
                check(queries.getAuthorName(MAX_UINT64)?.name == "Unsigned")
                queries.upsertAuthor(MAX_UINT64, "Unsigned", "Biography")
                check(queries.getAuthor(MAX_UINT64)?.bio == "Biography")
                val second = requireNotNull(queries.createAuthor(SECOND_ID, "Second", "Second bio"))
                check(second.bio == "Second bio")
                check(queries.listAuthors().map { it.id } == listOf(SECOND_ID, MAX_UINT64))
                queries.deleteAuthor(SECOND_ID)
                check(queries.getAuthor(SECOND_ID) == null)
                queries.deleteAuthor(MAX_UINT64)
                check(queries.listAuthors().isEmpty())
                retry.supplyStatus { session ->
                    val transaction = session.createNewTransaction(TxMode.SERIALIZABLE_RW)
                    val transactional = NativeQueries(transaction)
                    transactional.upsertAuthor(ROLLBACK_ID, "Rollback", null)
                    check(transactional.getAuthor(ROLLBACK_ID)?.name == "Rollback")
                    transaction.rollback()
                }.join().expectSuccess()
                check(queries.getAuthor(ROLLBACK_ID) == null)
                println("Kotlin native smoke passed")
            } finally {
                ddl("DROP TABLE authors;")
            }
        }
    }
}

private fun withSchema(endpoint: String, schema: String, body: (Connection) -> Unit) {
    DriverManager.getConnection("jdbc:ydb:$endpoint").use { connection ->
        connection.createStatement().use { it.execute(schema) }
        try {
            body(connection)
        } finally {
            if (!connection.autoCommit) {
                connection.rollback()
                connection.autoCommit = true
            }
            connection.createStatement().use { it.execute("DROP TABLE authors;") }
        }
    }
}

private fun runJdbc(endpoint: String, schema: String) = withSchema(endpoint, schema) { connection ->
    connection.autoCommit = false
    val queries = JdbcQueries(connection)
    check(queries.getAuthor(MAX_UINT64) == null)
    val created = requireNotNull(queries.createAuthor(MAX_UINT64, "Unsigned", null))
    check(created.id == MAX_UINT64 && created.name == "Unsigned" && created.bio == null)
    check(queries.getAuthorName(MAX_UINT64)?.name == "Unsigned")
    queries.upsertAuthor(MAX_UINT64, "Unsigned", "Biography")
    check(queries.getAuthor(MAX_UINT64)?.bio == "Biography")
    val second = requireNotNull(queries.createAuthor(SECOND_ID, "Second", "Second bio"))
    check(second.bio == "Second bio")
    check(queries.listAuthors().map { it.id } == listOf(SECOND_ID, MAX_UINT64))
    queries.deleteAuthor(SECOND_ID)
    check(queries.getAuthor(SECOND_ID) == null)
    check(!connection.isClosed && !connection.autoCommit)
    connection.commit()
    queries.upsertAuthor(ROLLBACK_ID, "Rollback", null)
    connection.rollback()
    check(queries.getAuthor(ROLLBACK_ID) == null)
    check(queries.getAuthor(MAX_UINT64)?.bio == "Biography")
    queries.deleteAuthor(MAX_UINT64)
    check(queries.listAuthors().isEmpty())
    connection.commit()
    println("Kotlin JDBC smoke passed")
}

private class ExpectedRollback : RuntimeException()

private fun runExposed(endpoint: String, schema: String) = withSchema(endpoint, schema) { observer ->
    registerYdbDialect()
    val database = Database.connect(
        url = "jdbc:ydb:$endpoint",
        driver = "tech.ydb.jdbc.YdbDriver",
        databaseConfig = DatabaseConfig { useNestedTransactions = false }
    )
    ydbTransaction(database) {
        val queries = ExposedQueries(this)
        check(queries.getAuthor(MAX_UINT64) == null)
        val created = requireNotNull(queries.createAuthor(MAX_UINT64, "Unsigned", null))
        check(created.id == MAX_UINT64 && created.name == "Unsigned" && created.bio == null)
        check(queries.getAuthorName(MAX_UINT64)?.name == "Unsigned")
        queries.upsertAuthor(MAX_UINT64, "Unsigned", "Biography")
        check(queries.getAuthor(MAX_UINT64)?.bio == "Biography")
        val second = requireNotNull(queries.createAuthor(SECOND_ID, "Second", "Second bio"))
        check(second.bio == "Second bio")
        check(queries.listAuthors().map { it.id } == listOf(SECOND_ID, MAX_UINT64))
        queries.deleteAuthor(SECOND_ID)
        check(queries.getAuthor(SECOND_ID) == null)
        check(!connection.isClosed)
        // Another connection must not see uncommitted generated writes.
        check(JdbcQueries(observer).getAuthor(MAX_UINT64) == null)
    }
    check(JdbcQueries(observer).getAuthor(MAX_UINT64)?.bio == "Biography")
    try {
        ydbTransaction(database) {
            val queries = ExposedQueries(this)
            queries.upsertAuthor(ROLLBACK_ID, "Rollback", null)
            check(queries.getAuthor(ROLLBACK_ID)?.name == "Rollback")
            throw ExpectedRollback()
        }
        error("Expected rollback exception")
    } catch (_: ExpectedRollback) {
        check(JdbcQueries(observer).getAuthor(ROLLBACK_ID) == null)
    }
    ydbTransaction(database) {
        val queries = ExposedQueries(this)
        queries.deleteAuthor(MAX_UINT64)
        check(queries.listAuthors().isEmpty())
    }
    println("Kotlin Exposed smoke passed")
}
