package endtoend

const dmlScriptsResultQueries = `
-- name: ReadBeforeMutating :one
DECLARE $id AS Uint64;
DECLARE $minimum AS Int64;
SELECT id, value, label FROM records WHERE id = $id AND value >= $minimum;
UPDATE records SET value = value + 10 WHERE id = $id;

-- name: ReadAfterMutating :many
DECLARE $id AS Uint64;
UPDATE records SET value = value + 10 WHERE id = $id;
SELECT id, value, label FROM records WHERE id = $id;

-- name: ReadBetweenMutations :many
DECLARE $id AS Uint64;
DECLARE $minimum AS Int64;
UPDATE records SET value = value + 10 WHERE id = $id;
SELECT id, value, label FROM records WHERE id = $id AND value >= $minimum;
UPDATE records SET value = value + 100 WHERE id = $id;

-- name: FailAfterResultOne :one
DECLARE $id AS Uint64;
DECLARE $minimum AS Int64;
SELECT id, value, label FROM records WHERE id = $id AND value >= $minimum;
UPDATE records SET value = value + 10 WHERE id = $id;
INSERT INTO copies (id, value, label) VALUES ($id, 999l, 'duplicate'u);

-- name: FailAfterResultMany :many
DECLARE $id AS Uint64;
DECLARE $minimum AS Int64;
SELECT id, value, label FROM records WHERE id = $id AND value >= $minimum;
UPDATE records SET value = value + 10 WHERE id = $id;
INSERT INTO copies (id, value, label) VALUES ($id, 999l, 'duplicate'u);

-- name: ReturnBeforeMutating :one
DECLARE $id AS Uint64;
UPDATE records SET value = value + 10 WHERE id = $id RETURNING id, value, label;
UPDATE records SET value = value + 100 WHERE id = $id;
`

const dmlScriptsResultGoRuntime = `
func checkMixedDMLScripts(t *testing.T,ctx context.Context,q *Queries,withTx func(func(context.Context,*Queries)error)error,counter *countedDB) {
 t.Helper()
 reset := func() {
  t.Helper()
  if err := q.DeleteTogether(ctx,4);err != nil {t.Fatal(err)}
  if err := q.MutateTogether(ctx,MutateTogetherParams{ID:4,Value:10,Label:"mixed",ObsoleteID:99});err != nil {t.Fatal(err)}
 }
 check := func(value int64) {
  t.Helper()
  if err := checkScriptRows(ctx,q,4,value,10,"mixed");err != nil {t.Fatal(err)}
 }
 oneRequest := func(before int) {
  t.Helper()
  if counter.queries != before+1 {t.Fatalf("result script used %d query calls, want one",counter.queries-before)}
 }
 reset()
 before := counter.queries
 first,err := q.ReadBeforeMutating(ctx,ReadBeforeMutatingParams{ID:4,Minimum:0})
 oneRequest(before)
 if err != nil || first.ID!=4 || first.Value!=11 || first.Label!="mixed" {t.Fatalf("SELECT before DML: %+v %v",first,err)}
 check(21)

 reset()
 before = counter.queries
 after,err := q.ReadAfterMutating(ctx,4)
 oneRequest(before)
 if err != nil || len(after)!=1 || after[0].ID!=4 || after[0].Value!=21 || after[0].Label!="mixed" {t.Fatalf("SELECT after DML: %+v %v",after,err)}
 check(21)

 reset()
 before = counter.queries
 middle,err := q.ReadBetweenMutations(ctx,ReadBetweenMutationsParams{ID:4,Minimum:0})
 oneRequest(before)
 if err != nil || len(middle)!=1 || middle[0].ID!=4 || middle[0].Value!=21 || middle[0].Label!="mixed" {t.Fatalf("SELECT between DML: %+v %v",middle,err)}
 check(121)

 reset()
 middle,err = q.ReadBetweenMutations(ctx,ReadBetweenMutationsParams{ID:4,Minimum:1000})
 if err != nil || len(middle)!=0 {t.Fatalf("empty :many: %+v %v",middle,err)}
 check(121)

 reset()
 _,err = q.ReadBeforeMutating(ctx,ReadBeforeMutatingParams{ID:4,Minimum:1000})
 if !errors.Is(err,$NO_ROWS) {t.Fatalf("empty :one: %v",err)}
 check(21)

 reset()
 err = withTx(func(ctx context.Context,tx *Queries)error {
  _,err := tx.ReadBeforeMutating(ctx,ReadBeforeMutatingParams{ID:4,Minimum:1000})
  return err
 })
 if !errors.Is(err,$NO_ROWS) {t.Fatalf("empty :one transaction: %v",err)}
 check(11)

 err = withTx(func(ctx context.Context,tx *Queries)error {
  rows,err := tx.ReadAfterMutating(ctx,4)
  if err != nil {return err}
  if len(rows)!=1 || rows[0].Value!=21 {return fmt.Errorf("mixed transaction result: %+v",rows)}
  return checkScriptRows(ctx,tx,4,21,10,"mixed")
 })
 if err != nil {t.Fatal(err)}
 check(21)

 reset()
 aborted := errors.New("caller rolls back mixed script")
 err = withTx(func(ctx context.Context,tx *Queries)error {
  rows,err := tx.ReadBetweenMutations(ctx,ReadBetweenMutationsParams{ID:4,Minimum:0})
  if err != nil {return err}
  if len(rows)!=1 || rows[0].Value!=21 {return fmt.Errorf("mixed transaction result: %+v",rows)}
  if err := checkScriptRows(ctx,tx,4,121,10,"mixed");err != nil {return err}
  return aborted
 })
 if !errors.Is(err,aborted) {t.Fatal(err)}
 check(11)

 for _,minimum := range []int64{0,1000} {
  _,err := q.FailAfterResultOne(ctx,FailAfterResultOneParams{ID:4,Minimum:minimum})
  if !ydb.IsOperationError(err,Ydb.StatusIds_PRECONDITION_FAILED) {t.Fatalf("late failure after :one minimum=%d: %v",minimum,err)}
  check(11)
  rows,err := q.FailAfterResultMany(ctx,FailAfterResultManyParams{ID:4,Minimum:minimum})
  if !ydb.IsOperationError(err,Ydb.StatusIds_PRECONDITION_FAILED) || len(rows)!=0 {t.Fatalf("late failure after :many minimum=%d: %+v %v",minimum,rows,err)}
  check(11)
 }
 err = withTx(func(ctx context.Context,tx *Queries)error {
  _,err := tx.FailAfterResultOne(ctx,FailAfterResultOneParams{ID:4,Minimum:0})
  return err
 })
 if !ydb.IsOperationError(err,Ydb.StatusIds_PRECONDITION_FAILED) {t.Fatalf("late mixed transaction failure: %v",err)}
 check(11)

 returned,err := q.ReturnBeforeMutating(ctx,4)
 if err != nil || returned.ID!=4 || returned.Value!=21 || returned.Label!="mixed" {t.Fatalf("RETURNING before DML: %+v %v",returned,err)}
 check(121)
}
`

const dmlScriptsResultJooqMethods = `
    private static void resetMixed(Queries queries) {
        queries.deleteTogether(ULong.valueOf(4));
        queries.mutateTogether(ULong.valueOf(4),10L,"mixed",ULong.valueOf(99));
    }
    private static void expectLateFailure(Runnable action) {
        try {
            action.run();
        } catch (org.jooq.exception.DataAccessException expected) {
            for (Throwable cause=expected; cause!=null; cause=cause.getCause()) {
                if (cause instanceof tech.ydb.jdbc.exception.YdbSQLException status && status.getStatus().getCode()==tech.ydb.core.StatusCode.PRECONDITION_FAILED) return;
            }
            throw new AssertionError("wrong late failure",expected);
        }
        throw new AssertionError("late duplicate INSERT returned success");
    }
    private static void checkMixed(Queries queries, java.sql.Connection connection) throws Exception {
        var id = ULong.valueOf(4);
        resetMixed(queries);
        var patched = queries.readPatchedValue(id,"mapped mixed").orElseThrow();
        if (!patched.equals(new ReadPatchedValueRow(id,11L,"mapped mixed")) || !queries.listCopies().isEmpty()) throw new AssertionError("mixed inferred binding/mapping");
        resetMixed(queries);
        var first = queries.readBeforeMutating(id,0L).orElseThrow();
        if (!first.equals(new ReadBeforeMutatingRow(id,11L,"mixed"))) throw new AssertionError("SELECT before DML");
        check(queries,4,21,10,"mixed");
        resetMixed(queries);
        if (!queries.readAfterMutating(id).equals(List.of(new ReadAfterMutatingRow(id,21L,"mixed")))) throw new AssertionError("SELECT after DML");
        check(queries,4,21,10,"mixed");
        resetMixed(queries);
        if (!queries.readBetweenMutations(id,0L).equals(List.of(new ReadBetweenMutationsRow(id,21L,"mixed")))) throw new AssertionError("SELECT between DML");
        check(queries,4,121,10,"mixed");
        resetMixed(queries);
        if (!queries.readBetweenMutations(id,1000L).isEmpty()) throw new AssertionError("empty :many");
        check(queries,4,121,10,"mixed");
        resetMixed(queries);
        if (queries.readBeforeMutating(id,1000L).isPresent()) throw new AssertionError("empty :one");
        check(queries,4,21,10,"mixed");
        resetMixed(queries);
        connection.setAutoCommit(false);
        if (queries.readBeforeMutating(id,1000L).isPresent()) throw new AssertionError("empty transaction :one");
        check(queries,4,21,10,"mixed");
        connection.rollback();
        connection.setAutoCommit(true);
        check(queries,4,11,10,"mixed");
        connection.setAutoCommit(false);
        if (!queries.readAfterMutating(id).equals(List.of(new ReadAfterMutatingRow(id,21L,"mixed")))) throw new AssertionError("mixed transaction result");
        check(queries,4,21,10,"mixed");
        connection.commit();
        connection.setAutoCommit(true);
        check(queries,4,21,10,"mixed");
        resetMixed(queries);
        connection.setAutoCommit(false);
        if (!queries.readBetweenMutations(id,0L).equals(List.of(new ReadBetweenMutationsRow(id,21L,"mixed")))) throw new AssertionError("mixed transaction result");
        check(queries,4,121,10,"mixed");
        connection.rollback();
        connection.setAutoCommit(true);
        check(queries,4,11,10,"mixed");
        for (long minimum : new long[]{0,1000}) {
            expectLateFailure(() -> queries.failAfterResultOne(id,minimum));
            check(queries,4,11,10,"mixed");
            expectLateFailure(() -> queries.failAfterResultMany(id,minimum));
            check(queries,4,11,10,"mixed");
        }
        connection.setAutoCommit(false);
        expectLateFailure(() -> queries.failAfterResultOne(id,0L));
        connection.rollback();
        connection.setAutoCommit(true);
        check(queries,4,11,10,"mixed");
        if (!queries.returnBeforeMutating(id).orElseThrow().equals(new ReturnBeforeMutatingRow(id,21L,"mixed"))) throw new AssertionError("RETURNING before DML");
        check(queries,4,121,10,"mixed");
    }
`
