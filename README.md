# ndl — NDLサーチ API CLI

国立国会図書館サーチ（NDLサーチ）の外部提供API（SRU / OpenSearch / OAI-PMH）をラップするコマンドラインツールです。

> 本ツールは国立国会図書館サーチのAPIを利用しています。本ツールは国立国会図書館が提供・保証するものではありません。

- 準拠仕様: [外部提供インタフェース仕様書 第1.5版（2026-09-25）](https://ndlsearch.ndl.go.jp/help/api/specifications)
- メタデータ形式: [DC-NDL（RDF）フォーマット仕様](https://ndlsearch.ndl.go.jp/renkei/dcndl)
- 利用条件: [APIのご利用について](https://ndlsearch.ndl.go.jp/help/api)

## インストール

Go 1.22 以降が必要です。

```sh
go install github.com/nightswinger/ndl/cmd/ndl@latest
# またはリポジトリを clone して
go build ./cmd/ndl
```

バージョン文字列（User-Agent に入る）を埋め込む場合:

```sh
go build -ldflags "-X github.com/nightswinger/ndl/internal/cli.Version=0.1.0" ./cmd/ndl
```

## 使い方

### SRU — `ndl sru <CQL>`

```sh
# CQL をそのまま渡す
ndl sru 'title="桜" AND from="2018"' --max 20

# 補助フラグから CQL を組み立てる（AND 結合）。CQL 引数との併用はエラー
ndl sru --title 桜 --from 2018 --dpid iss-ndl-opac
ndl sru --isbn 9784101010014 --format json
ndl sru --jpno 20570720

# 全件ページング（サーバ仕様により 500 件目まで）
ndl sru 'creator="夏目漱石"' --all --limit 300 --format jsonl

# 送信せず URL だけ確認
ndl sru 'title="桜"' --dry-run
```

| フラグ | API パラメータ | 備考 |
|---|---|---|
| `--start` | startRecord | 既定 1 |
| `--max` | maximumRecords | 1〜500、既定 20 |
| `--schema` | recordSchema | `dcndl`（既定）/ `dcndl_v3` / `dc` |
| `--packing` | recordPacking | `xml`（既定）/ `string` |
| `--only-bib` | onlyBib=true | dcndl / dcndl_v3 のみ |
| `--inprocess` | inprocess=true | 新着書誌情報のみ |
| `--all`, `--limit` | — | nextRecordPosition を追従 |
| `--title --creator --publisher --anywhere --isbn --issn --jpno --ndc --from --until --dpid --mediatype` | CQL | `--dpid` と `--mediatype` はカンマ区切りで複数指定（OR） |

- 応答の `<diagnostics>` は必ずパースします。診断があればメッセージを stderr に出して終了コード 1 で終わります。ただし NDL は 0 件のときも診断「Record does not exist」を返すため、これだけは 0 件として扱い、終了コード 0 にします。
- `numberOfRecords` は stderr に出ます（`--quiet` で抑止）。

### OpenSearch — `ndl opensearch`（エイリアス `ndl os`）

```sh
ndl os --title 桜 --from 2018 --cnt 20
ndl os --creator 夏目 --dpid aozora --format json
ndl os --isbn 9784101010014
```

フラグは仕様書の表4-1 と 1対1 で対応しています: `--title --creator --publisher --digitized_publisher --ndc --from --until --isbn --mediatype --dpid --dpgroupid --any --cnt --idx`（`--cnt` は 1〜500、既定 20）。

OpenSearch はパラメータの誤りをエラーとして返しません。そのため ndl は次の場合に stderr に警告を出します。

- **0 件**のとき: 値の誤り（`cnt` が 500 を超える、`from` と `until` の書式が揃っていない、存在しない dpid など）の可能性を知らせます。
- 条件を指定したのに**ヒット件数が極端に多い**とき: パラメータ名の誤りの可能性を知らせます。未知のパラメータ名はサーバ側で黙って無視され、全件検索になります。
- 未知のフラグ（例: `--titel`）は cobra 側で弾き、終了コード 2 になります。
- `--issn` と `--jpno` は OpenSearch API に存在しません。送ると無視されて全件ヒットになるため、エラーにして `ndl sru --issn/--jpno` を案内します。

### OAI-PMH — `ndl oaipmh <verb>`（エイリアス `ndl harvest`）

```sh
ndl oaipmh identify
ndl oaipmh formats
ndl oaipmh sets
ndl oaipmh identifiers --set iss-ndl-opac --from 2026-10-01 --until 2026-10-01
ndl oaipmh records --set aozora --from 2026-01-01 --limit 500 --format jsonl
ndl oaipmh get R000000014-I000005card5000005 --prefix dcndl --format json

# 長時間ハーベスト: jsonl を追記し、進捗を stderr に出す
ndl harvest records --set iss-ndl-opac --from 2026-10-01 --until 2026-10-31 --out ndl.jsonl
# Ctrl-C で中断すると再開用のコマンドを表示する
ndl harvest records --token 'dcndl/2026-10-01T00:00:00Z/...' --out ndl.jsonl
```

| フラグ | 内容 |
|---|---|
| `--prefix` | metadataPrefix: `dcndl`（既定）/ `dcndl_v3` / `oai_dc` |
| `--from`, `--until` | `YYYY-MM-DD` または `YYYY-MM-DDThh:mm:ssZ`（JST 扱い）。**records / identifiers では `--from` が必須** |
| `--set` | データプロバイダID、データグループID、コレクションコード、公開範囲 |
| `--token` | resumptionToken から再開（他の絞り込み条件は送らない） |
| `--limit N` | N 件で打ち切る |
| `--follow=false` | 1 ページ（200 件）だけ取得 |
| `--out FILE` | JSON Lines で FILE に追記 |

- `records` と `identifiers` は resumptionToken を自動で追従し、ページ間では `--interval` だけ待機します。
- ページごとに進捗（取得件数 / completeListSize と現在の token）を stderr に出します。
- 中断・エラー・`--limit` で止まったときは、再開用の `--token` を表示します。
- `<error code="...">` はパースして終了コード 1 にします。ただし `noRecordsMatch` は 0 件として扱います。
- 指定できる期間は最大 1 年です。`--until` を省略すると、サーバは from + 1 年として扱います。

### 共通フラグ

| フラグ | 内容 |
|---|---|
| `--format table\|json\|jsonl\|xml` | 既定 table。xml は API の生レスポンスをそのまま出力 |
| `--fields` | table / json / jsonl の項目を絞る（例: `title,creator,date,identifier,url`） |
| `--dry-run` | リクエスト URL を表示するだけで送信しない |
| `--base-url` | 既定 `https://ndlsearch.ndl.go.jp/api`（環境変数 `NDL_BASE_URL`） |
| `--timeout` | 1 リクエストのタイムアウト。既定 30s（環境変数 `NDL_TIMEOUT`。`30s` または秒数） |
| `--interval` | 連続するリクエストの最小間隔。既定 1s |
| `--verbose`, `-v` | リクエストのログを stderr に出す |
| `--quiet`, `-q` | 件数・進捗を出さない |

フィールド名: `id title title_transcription creators publishers date isbn issn jpno ndlbibid ndc ndlc subjects material_type language url see_also oai_identifier datestamp sets deleted`。加えて `identifier`（JP番号 → ISBN → ISSN → アイテムID の順で最初にあるもの）を指定できます。別名として `creator`、`publisher`、`author` も使えます。

json 出力の形は次のとおりです。

```json
{
  "meta": { "source": "sru", "query": "...", "numberOfRecords": 11625, "nextRecordPosition": 4, "returned": 3 },
  "records": [ { "id": "R100000002-I000007305653", "title": "吾輩は猫である", "creators": ["夏目, 漱石, 1867-1916"], "...": "..." } ]
}
```

OAI-PMH のときは `meta.resumptionToken` が付きます。

### 終了コード

| コード | 意味 |
|---|---|
| 0 | 成功（0 件を含む） |
| 1 | API の診断・エラー（SRU diagnostics、OAI-PMH error、HTTP 4xx/5xx） |
| 2 | 引数の誤り |
| 3 | ネットワークエラー（接続失敗・タイムアウト） |
| 130 | Ctrl-C による中断 |

## サーバへの負荷について

- User-Agent は `ndl-cli/<version> (+https://github.com/nightswinger/ndl-cli)` です。
- 連続するリクエストの間は必ず `--interval`（既定 1 秒）以上空けます。`--all` やハーベストでも同じです。
- 5xx・接続エラー・**429 Too Many Requests**（NDL の同時アクセス数の上限）は、指数バックオフで最大 3 回リトライします。それ以外の 4xx はリトライしません。
- NDL は「多重アクセスが生じないよう」求めています。このツールを並列に起動しないでください。継続的にアクセスする場合は、申請の要否に関係なく [NDLサーチAPI利用申請フォーム](https://ndlsearch.ndl.go.jp/help/api) から連絡するよう依頼されています。

## NDLサーチ API の利用条件（要約）

詳しくは必ず公式の [APIのご利用について](https://ndlsearch.ndl.go.jp/help/api) と [API提供対象データプロバイダ一覧](https://ndlsearch.ndl.go.jp/help/api/provider) を確認してください。以下は 2026-10-21 時点の要約です。

- **クレジット表示**: API を利用するサイトやアプリには、NDLサーチの API を使っていることを明記します。データプロバイダによっては、その機関のクレジット表示（CC BY など）も必要です。
  - 表示例:「本サービスで提供するメタデータの一部は、国立国会図書館サーチのAPIから取得した［データプロバイダ名］（［データ提供機関］が運営）に由来します。ライセンスはクリエイティブ・コモンズ 表示 4.0 国際 パブリック・ライセンスです。」
- **営利目的**（広告収入など、何らかの収益を得る場合を含む）の利用には**利用申請**が必要です。ただし、データ提供機関が目的を問わず利用を許諾しているデータ（CC BY、CC0 など）は申請不要です。
- **個人・非営利**で収益を得ない利用は、申請不要です。
- **同時リクエスト数には制限があります**。継続的な大量アクセスは遮断されることがあります。

### 営利利用に申請が必要なデータプロバイダ（dpid）

一覧で「営利: △」となっているもの（2026-10-21 時点）:

`shinsho` `awareness` `irdb` `penclub` `tsukuba` `okinawa-1` `okinawa-2` `waseda` `hokkaido` `osaka-n` `kawasakishi` `toyama` `gifu` `miyada` `shizuoka` `hakodate` `miyazaki` `shiga` `niigata` `sapie` `ndl-dl` `ndl-dl-online` `ndl-dl-doi` `tenroku` `nosui` `okinawa-rare-digital` `komazawa-univ` `nosui-library-da` `s-court` `ciniir` `jpro` `jpro-book` `jpro-online` `jpro-audiobook` `jpro-online-repository` `jpro-online-tts` `nagoya` `osaka-jibunkan` `irdb-pub`

その他の注意:

- **非営利ライセンス**: `jaea`（CC BY-NC）、`shirayuri`・`hiroshima`（CC BY-NC-ND）、`minamata`（CC BY-NC-SA）は営利利用できません。
- **J-STAGE**（`jstage`）の抄録（`dcterms:abstract`）は一律で利用できません。
- **検索用APIのみ（OAI-PMH 非対応）**: `shinsho` `awareness` `irdb` `penclub` `sapie` `tenroku` `osaka-jibunkan` `irdb-pub` `hiroshima`
- dpid の代わりに `open` を指定すると、PDM / CC0 で利用できるメタデータだけを横断して検索・取得できます。
- 国立国会図書館が作成した書誌（`iss-ndl-opac`、`zassaku` など）は CC BY 4.0 互換です。

## 公式仕様と当初想定との差異

仕様書と実 API で確認した内容です。

- SRU の recordSchema は `dc` / `dcndl` / `dcndl_v3` だけです。`dcndl_simple` と `oai_dc` は「illegal recordSchema value」になります。
- OAI-PMH の metadataPrefix は `oai_dc` / `dcndl` / `dcndl_v3` だけです（`dcndl_simple` は cannotDisseminateFormat）。
- SRU と OpenSearch の 1 回あたりの件数は最大 500 件です。また、501 件目以降はページングしても取得できません。
- OAI-PMH の ListRecords と ListIdentifiers は `from` が必須で、期間は最大 1 年、1 ページ 200 件です。
- OpenSearch に `issn` / `jpno` パラメータはありません。また `dpgroupid` は 2023 年 12 月に廃止されています。
- 未知のパラメータ名は無視されて全件ヒットになり、値の誤りは 0 件になります。

## 開発

```sh
go test ./...                                   # testdata/ の実応答フィクスチャを httptest で返す
go test -tags integration ./internal/cli/       # 実 API に数リクエスト送る統合テスト
```

`testdata/` には、2026-10-21 に実 API から取得した応答 XML を保存しています。

構成:

```
cmd/ndl/            エントリポイント
internal/cli/       cobra コマンド定義
internal/client/    共通 HTTP クライアント（User-Agent、タイムアウト、リトライ、間隔制御）
internal/sru/       SRU リクエスト構築・CQL 組み立て・diagnostics・ページング
internal/opensearch/ OpenSearch リクエスト構築・RSS パース・警告
internal/oaipmh/    OAI-PMH 各 verb・resumptionToken 追従
internal/dcndl/     DC-NDL / DC レコードの共通構造体とパーサ
internal/output/    table / json / jsonl 出力
testdata/           実 API 応答フィクスチャ
```
