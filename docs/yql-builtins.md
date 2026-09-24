# YQL built-in coverage inventory

This inventory is pinned to upstream YDB commit `1415fed8104201c5e973dd8bbf12c71c6b1ed8b9`. It records all 193 level-two reference sections across the ten built-in reference pages. A section can describe several functions, aliases, or non-function syntax; 193 is not a count of supported functions. The separately linked C++ UDF libraries are outside this count.

A resolved subset means that the offline analyzer has explicit type rules for the stated forms. It does not imply every overload, SQL context, or output SDK is supported. Argument resolution, callable/type/resource values, aggregate/window context, and selected-runtime binding/decoding remain independent requirements. Unknown names or unsupported forms fail with diagnostics; catalog presence is never a fallback return type.

The current implementation keeps one shared semantic pass before code generation. Full reference coverage requires further work on lambda and callable forms outside the supported subset, type-valued expressions, variants/resources, window frames, collection/struct overloads, SQL/JSON clauses, and compile-time code generation. Those prerequisites must be implemented and verified before the corresponding entries can be marked supported.

Prerequisite labels below are work still required, not supported overloads: **S** scalar overload/type rule; **C** collection/struct construction or reconciliation; **L** scoped lambda/callable analysis; **T** type-valued expressions; **V** Variant/Enum/Tagged value types; **R** resource/code/world values; **M** table/provider metadata context; **W** window/frame context; **A** aggregate state, arguments and empty/grouped semantics; **K** literal-name/option metadata; **J** SQL/JSON grammar and ON EMPTY/ERROR rules; **F** provider feature or external-file context.

## Basic

[55 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/basic.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| COALESCE | Resolved subset: Pairwise numeric reconciliation; fitting right-hand integer literals preserve the left integer type; a direct `ListCreate` fallback can take the preceding List type; optionality follows fallback availability. |
| LENGTH | Resolved subset: LENGTH/LEN; byte count. |
| SUBSTRING | Resolved subset: String input; bounded unsigned positions through Uint32. |
| FIND | Resolved subset: String/Utf8 input; bounded unsigned position. |
| RFIND | Resolved subset: String/Utf8 input; bounded unsigned position. |
| StartsWith, EndsWith | Resolved subset: Matching String/Utf8 types and Optional propagation. |
| IF | Resolved subset: Two/three arguments with resolved branch types. |
| NANVL | Resolved subset: Float/Double inputs and Optional propagation. |
| Random... | Resolved subset: Random, RandomNumber, RandomUuid; at least one typed dependency argument. |
| CurrentUtc... | Resolved subset: CurrentUtcDate, CurrentUtcDatetime, CurrentUtcTimestamp; typed dependencies. |
| CurrentTz... | Resolved subset: CurrentTzDate/Datetime/Timestamp; String/Optional<String>/NULL zone, typed dependencies, Optional<Tz*> result. |
| AddTimezone | Not implemented — S: date-family/zone overloads and invalid-zone nullability. |
| RemoveTimezone | Not implemented — S: timezone-family conversion. |
| Version | Resolved subset: Zero arguments; String result. |
| MAX_OF, MIN_OF, GREATEST, and LEAST | Not implemented — S: common-type and NULL behavior. |
| AsTuple, AsStruct, AsList, AsDict, AsSet, AsListStrict, AsDictStrict and AsSetStrict | Resolved subset: Nonempty AsTuple, named-field AsStruct and common-type AsList; empty AsList is accepted directly inside Json::From/Yson::From. Other constructors remain C, K. |
| Container literals | Parser syntax is not general typed container-expression support. |
| Variant | Not implemented — T, V, K: variant alternatives and selected member. |
| AsVariant | Not implemented — V, K: alternative identity. |
| Visit, VisitOrDefault | Not implemented — V, L: branch parameter scopes and result reconciliation. |
| VariantItem | Not implemented — V: alternative payload reconciliation. |
| Way | Not implemented — V: alternative identity/result type. |
| DynamicVariant | Not implemented — T, V: runtime-selected alternative. |
| Enum | Not implemented — T, V, K: enum alternatives. |
| AsEnum | Not implemented — V, K: literal alternative identity. |
| AsTagged, Untag | Not implemented — V, K: retained tag identity. |
| TablePath | Not implemented — M: current row/provider path. |
| TableName | Not implemented — M, S: provider path/name conventions. |
| TableRecordIndex | Not implemented — M: provider row index. |
| TableRow, JoinTableRow | Not implemented — M, C: relation/join row structure. |
| FileContent and FilePath | Not implemented — F: query file dependencies. |
| FolderPath | Not implemented — F: provider folder context. |
| ParseFile | Not implemented — F, T: external content and element type. |
| WeakField | Not implemented — M, T: weakly typed row fields. |
| Ensure... | Not implemented — S, T: assertion forms and error/nullability contract. |
| EvaluateExpr, EvaluateAtom | Not implemented — R, K: compile-time evaluation. |
| Literals of primitive types | Not implemented — S, K: constructor-form literal validation beyond existing SQL literals. |
| Access to the metadata of the current operation | Not implemented — M: provider operation metadata. |
| ToBytes and FromBytes | Not implemented — S, T: serialization and requested target type. |
| ByteAt | Not implemented — S: byte/index types and out-of-range nullability. |
| ...Bit | Not implemented — S: integer widths, shifts and rotations. |
| Abs | Resolved subset: Supported primitive numeric/Decimal input. |
| Just | Not implemented — S: Optional construction including nested levels. |
| Unwrap | Resolved subset: One Optional layer removed; an optional message argument must have String or Utf8 type. |
| Nothing | Not implemented — T: target Optional type. |
| Callable | Not implemented — T, L: callable signature and scoped parameters. |
| Pickle, Unpickle | Not implemented — T, R: serialization and requested target type. |
| StaticMap | Not implemented — C, L: compile-time element mapping. |
| StaticZip | Not implemented — C: structural static zip. |
| StaticFold, StaticFold1 | Not implemented — C, L: accumulator and element scopes. |
| AggregationFactory | Not implemented — A, R, K: named aggregate factory state. |
| AggregateTransformInput | Not implemented — A, R, L: typed input transformation. |
| AggregateTransformOutput | Not implemented — A, R, L: typed result transformation. |
| AggregateFlatten | Not implemented — A, R: nested aggregate state. |
| YQL::, s-expressions | Not implemented — R: lower-level expression language. |

## Aggregation

[23 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/aggregation.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| COUNT | Resolved subset: Uint64; COUNT(*) and typed argument; DISTINCT not yet supported. |
| MIN and MAX | Resolved subset: Supported comparable scalar subset; empty/grouped nullability. |
| SUM | Resolved subset: Supported numeric subset; widening and empty/grouped nullability. |
| AVG | Resolved subset: Supported numeric/Interval subset; empty/grouped nullability. |
| COUNT_IF | Resolved subset: Bool/Optional<Bool>/NULL input; Uint64 result, including empty input. |
| SUM_IF and AVG_IF | Not implemented — A: argument/result types and empty/grouped behavior. |
| SOME | Not implemented — A: argument/result types and empty/grouped behavior. |
| CountDistinctEstimate, HyperLogLog, and HLL | Not implemented — A: argument/result types and empty/grouped behavior. |
| AGGREGATE_LIST | Resolved subset: List of the non-null input element type, optional checked integer limit, grouped and empty-input behavior. |
| MAX_BY and MIN_BY | Not implemented — A, C: aggregate collection/result structure and empty/grouped behavior. |
| TOP and BOTTOM | Not implemented — A, C: aggregate collection/result structure and empty/grouped behavior. |
| TOP_BY and BOTTOM_BY | Not implemented — A, C: aggregate collection/result structure and empty/grouped behavior. |
| TOPFREQ and MODE | Not implemented — A, C: aggregate collection/result structure and empty/grouped behavior. |
| STDDEV and VARIANCE | Not implemented — A: argument/result types and empty/grouped behavior. |
| CORRELATION and COVARIANCE | Not implemented — A: argument/result types and empty/grouped behavior. |
| PERCENTILE and MEDIAN | Not implemented — A: argument/result types and empty/grouped behavior. |
| HISTOGRAM | Resolved subset: Numeric input, optional weighted/bucket arguments and the documented nullable HistogramStruct result. |
| LinearHistogram, LogarithmicHistogram, and LogHistogram | Resolved subset: Numeric input and the documented nullable HistogramStruct result. |
| CDF (cumulative distribution function) | Resolved subset: Histogram CDF aggregate aliases return the nullable HistogramStruct type. |
| BOOL_AND, BOOL_OR and BOOL_XOR | Not implemented — A: argument/result types and empty/grouped behavior. |
| BIT_AND, BIT_OR and BIT_XOR | Not implemented — A: argument/result types and empty/grouped behavior. |
| SessionStart | Not implemented — A, W: session grouping context. |
| AGGREGATE_BY and MULTI_AGGREGATE_BY | Not implemented — A, R, L: aggregate factory and input/output contracts. |

## List

[35 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/list.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| ListCreate | Resolved subset: Literal element type and contextual empty-list fallback in COALESCE/NVL. |
| AsList and AsListStrict | Resolved subset: AsList with common element type; empty AsList only directly inside Json::From/Yson::From. AsListStrict remains C. |
| ListLength | Resolved subset: List<T> and Optional<List<T>>; nullable input produces Optional<Uint64>. |
| ListHasItems | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListCollect | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListSort, ListSortAsc, and ListSortDesc | Not implemented — C, L: comparable elements and optional key-selector lambda. |
| ListExtend and ListExtendStrict | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListUnionAll | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListZip and ListZipAll | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListEnumerate | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListReverse | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListSkip | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListTake | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListSample and ListSampleN | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListShuffle | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListIndexOf | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListMap, ListFilter, and ListFlatMap | Resolved subset: Single-argument typed lambda or local callable for ListMap/ListFilter; ListFlatMap remains C, L. |
| ListNotNull | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListFlatten | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListUniq | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListAny and ListAll | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListHas | Resolved subset: Equatable typed list element with matching or String/Utf8 search type, including optional search values; NULL outer list returns Bool false. |
| ListHead, ListLast | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListMin, ListMax, ListSum and ListAvg | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListFold, ListFold1 | Not implemented — C, L: scoped element/accumulator types and callback result. |
| ListFoldMap, ListFold1Map | Not implemented — C, L: scoped element/accumulator types and callback result. |
| ListFromRange | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListReplicate | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListConcat | Not implemented — C: element types, Optional/empty behavior and operation-specific arguments. |
| ListExtract | Not implemented — C, K: literal Struct member and output element type. |
| ListTakeWhile, ListSkipWhile | Not implemented — C, L: scoped element/accumulator types and callback result. |
| ListAggregate | Not implemented — C, A, R: typed aggregation factory. |
| ToDict and ToMultiDict | Resolved subset: List<Tuple<K,V>> to Dict<K,V>, preserving outer optionality; ToMultiDict remains C. |
| ToSet | Resolved subset: Concrete list keys; supported dictionary-key subset. |
| ListTop, ListTopAsc, ListTopDesc, ListTopSort, ListTopSortAsc и ListTopSortDesc | Not implemented — C, L: comparable elements and optional key-selector lambda. |

## Dict

[16 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/dict.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| DictCreate | Not implemented — T, C: requested key/payload types. |
| SetCreate | Not implemented — T, C: requested key/payload types. |
| DictLength | Not implemented — C: key/payload types and lookup/set nullability. |
| DictHasItems | Not implemented — C: key/payload types and lookup/set nullability. |
| DictItems | Not implemented — C: key/payload types and lookup/set nullability. |
| DictKeys | Not implemented — C: key/payload types and lookup/set nullability. |
| DictPayloads | Not implemented — C: key/payload types and lookup/set nullability. |
| DictLookup | Resolved subset: Matching or String/Utf8 key search type, including optional search values; preserves nested payload optionality and adds one Optional layer for lookup. |
| DictContains | Resolved subset: Matching or String/Utf8 key search type, including optional search values; NULL outer dictionary returns Bool false. |
| DictAggregate | Not implemented — C, A, L: aggregation callbacks and output payload. |
| SetIsDisjoint | Resolved subset: Resolved matching key types. |
| SetIntersection | Not implemented — C: key/payload types and lookup/set nullability. |
| SetIncludes | Not implemented — C: key/payload types and lookup/set nullability. |
| SetUnion | Not implemented — C: key/payload types and lookup/set nullability. |
| SetDifference | Not implemented — C: key/payload types and lookup/set nullability. |
| SetSymmetricDifference | Not implemented — C: key/payload types and lookup/set nullability. |

## Struct

[17 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/struct.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| TryMember | Not implemented — C, K: literal member names and structural result reconciliation. |
| ExpandStruct | Not implemented — C, K: literal member names and structural result reconciliation. |
| AddMember | Not implemented — C, K: literal member names and structural result reconciliation. |
| RemoveMember | Not implemented — C, K: literal member names and structural result reconciliation. |
| ForceRemoveMember | Not implemented — C, K: literal member names and structural result reconciliation. |
| ChooseMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| RemoveMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| ForceRemoveMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| CombineMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| FlattenMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| StructMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| RenameMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| ForceRenameMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| GatherMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| SpreadMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| ForceSpreadMembers | Not implemented — C, K: literal member names and structural result reconciliation. |
| StructUnion, StructIntersection, StructDifference, StructSymmetricDifference | Not implemented — C, L: structural reconciliation and optional member-merge lambda. |

## Types

[21 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/types.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| FormatType | Not implemented — T: compile-time type values and type introspection. |
| ParseType | Not implemented — T: compile-time type values and type introspection. |
| TypeOf | Not implemented — T: compile-time type values and type introspection. |
| InstanceOf | Not implemented — T: compile-time type values and type introspection. |
| DataType | Not implemented — T: compile-time type values and type introspection. |
| OptionalType | Not implemented — T: compile-time type values and type introspection. |
| ListType and StreamType | Not implemented — T: compile-time type values and type introspection. |
| DictType | Not implemented — T: compile-time type values and type introspection. |
| TupleType | Not implemented — T: compile-time type values and type introspection. |
| StructType | Not implemented — T: compile-time type values and type introspection. |
| VariantType | Not implemented — T: compile-time type values and type introspection. |
| ResourceType | Not implemented — T: compile-time type values and type introspection. |
| CallableType | Not implemented — T: compile-time type values and type introspection. |
| GenericType, UnitType, and VoidType | Not implemented — T: compile-time type values and type introspection. |
| OptionalItemType, ListItemType and StreamItemType | Not implemented — T: compile-time type values and type introspection. |
| DictKeyType and DictPayloadType | Not implemented — T: compile-time type values and type introspection. |
| TupleElementType | Not implemented — T: compile-time type values and type introspection. |
| StructMemberType | Not implemented — T: compile-time type values and type introspection. |
| CallableResultType and CallableArgumentType | Not implemented — T: compile-time type values and type introspection. |
| VariantUnderlyingType | Not implemented — T: compile-time type values and type introspection. |
| Functions for data types during calculations | Not implemented — T: compile-time type values and type introspection. |

## Window

[9 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/window.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| Aggregate functions | Existing aggregates do not imply OVER/window support. |
| ROW_NUMBER | Not implemented — W: partition/frame context and operation-specific nullability. |
| LAG / LEAD | Not implemented — W: partition/frame context and operation-specific nullability. |
| FIRST_VALUE / LAST_VALUE | Not implemented — W: partition/frame context and operation-specific nullability. |
| NTH_VALUE | Not implemented — W: partition/frame context and operation-specific nullability. |
| RANK / DENSE_RANK / PERCENT_RANK | Not implemented — W: partition/frame context and operation-specific nullability. |
| NTILE | Not implemented — W: partition/frame context and operation-specific nullability. |
| CUME_DIST | Not implemented — W: partition/frame context and operation-specific nullability. |
| SessionState() | Not implemented — W: partition/frame context and operation-specific nullability. |

## Codegen

[9 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/codegen.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| FormatCode | Not implemented — R, L: compile-time code values and scoped code construction/evaluation. |
| WorldCode | Not implemented — R, L: compile-time code values and scoped code construction/evaluation. |
| AtomCode | Not implemented — R, L: compile-time code values and scoped code construction/evaluation. |
| ListCode | Not implemented — R, L: compile-time code values and scoped code construction/evaluation. |
| FuncCode | Not implemented — R, L: compile-time code values and scoped code construction/evaluation. |
| LambdaCode | Not implemented — R, L: compile-time code values and scoped code construction/evaluation. |
| EvaluateCode | Not implemented — R, L: compile-time code values and scoped code construction/evaluation. |
| ReprCode | Not implemented — R, L: compile-time code values and scoped code construction/evaluation. |
| QuoteCode | Not implemented — R, L: compile-time code values and scoped code construction/evaluation. |

## Json

[6 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/json.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| JsonPath | Reference context, not a standalone function; SQL/JSON support is not implemented. |
| Common arguments | Reference context, not a standalone function; SQL/JSON support is not implemented. |
| JSON_EXISTS | Not implemented — J: path/RETURNING/PASSING and ON EMPTY/ERROR semantics. |
| JSON_VALUE | Not implemented — J: path/RETURNING/PASSING and ON EMPTY/ERROR semantics. |
| JSON_QUERY | Not implemented — J: path/RETURNING/PASSING and ON EMPTY/ERROR semantics. |
| See also | Reference context, not a standalone function; SQL/JSON support is not implemented. |

## Fulltext

[2 reference sections](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/fulltext.md).

| Reference section | Current coverage / prerequisite |
| --- | --- |
| FulltextMatch | Not implemented — F, S: server full-text capability and argument/result rules. |
| FulltextScore | Not implemented — F, S: server full-text capability and argument/result rules. |
