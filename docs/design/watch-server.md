# mping server / watch 設計書

- 状態: ドラフト（レビュー待ち、未実装）
- 作成日: 2026-11-22
- 更新日: 2026-11-22（huma を使わない決定、root なし ICMP の実装を反映）
- 前提にしたコード: v0.15.0

各節の印は次の意味です。

- **[合意]** 議論で決まったこと
- **[案]** まだ合意していない提案

## 1. 目的

特定のタイミングで、特定の場所から、特定の宛先へ probe を打ち、離れたところから結果を見続けられるようにする。**[合意]**

- 運用中の作業や調査のたびに watch を作り、attach して結果を見る。
- AI が今の状態と、前回からの変化を取得できる。
- 自分用（手元、踏み台）でも、チーム共有（k8s など）でも使える。

### やらないこと

- 定常的なサービス監視とアラート通知（cloudprober や Prometheus の役割）
- 複数 replica での冗長化。場所ごとに server を1つ置く。
- API から prober の設定を作成・変更すること

### 代表的な使い方

1. 踏み台で server を動かし、スイッチ更新の間だけ watch を作る。
2. 手元から attach し、切替のときに mark を付ける。
3. AI に「切替の前後で何が変わったか」を聞く。
4. 作業後に watch を止める。結果は後から読める。

## 2. 用語

| 用語 | 意味 |
|---|---|
| server | probe を打つ場所で動くプロセス |
| watch | 目的、ターゲット、間隔、期限のまとまり。互いに独立する |
| target | 宛先。今の mping の書式のまま |
| prober | probe の種類と設定。server の設定ファイルで定義する |
| event | 停止、復旧、TTL や接続先の変化などの出来事 |
| outage | 3回以上連続した失敗（今の定義のまま） |
| mark | 人や AI が付ける印。例「切替開始」 |
| summary | watch の今の要約 |
| attach | 動いている watch の画面を開くこと。閉じても watch は続く |

`watch` という名前は **[合意]**、ほかの用語は **[案]**。

k8s の watch（変更の購読）とは意味が違う。コード内の `session` は、1つの watch の中身を動かす内部の型として残す。

## 3. CLI **[案]**

```
mping server [--config FILE] [--listen ADDR] [--db FILE]
mping watch start --name NAME --for 2h [-i 1000] [-t 1000] [-f FILE] TARGET...
mping watch ls
mping watch attach NAME
mping watch mark NAME "切替開始"
mping watch stop NAME
mping watch rm NAME
```

- 接続先は `--server`、または環境変数 `MPING_SERVER` で選ぶ。
- `-f` はクライアント側のファイルを読み、内容だけを送る。server のファイルを読む API は作らない。
- `mping server` と `mping watch` を足すと、`server` や `watch` という名前のホストは、そのままでは指定できなくなる（今の `batch` と同じ）。

## 4. watch のライフサイクル **[案]**

```
start -> running -> stopped -> (rm / 保存期限切れ) 削除
              |         ^
              +- 期限切れ +
```

- 期限は必須。既定2時間、上限は server の設定で決める。
- `stop` と期限切れでは probe を止め、結果は残す。
- 一度止めた watch は再開しない。続きが必要なら新しく作る。
- 名前は、残っている watch の中で一意とする。API では変わらない ID も使える。

### server の再起動

- 期限内の `running` は自動で再開する。
- 止まっていた区間は `gap` イベントとして記録し、**outage とは区別する**。
- 再起動の前後の失敗を、1つの連続した失敗として数えない。
- 停止中に再起動した場合、その outage は「終わりが不明」として閉じる。

## 5. API **[案]**

HTTP + JSON とし、標準の `net/http` だけで作る（`ServeMux` のメソッド付きパターンを使う）。MCP は同じ処理を呼ぶ薄い層にする。

**huma などの HTTP ライブラリと OpenAPI は使わない。[合意]** 使うのは mping 自身の CLI と MCP であり、OpenAPI を公開する利点が薄いため。

- 入力の検査、エラーの形、JSON の上限サイズは自前で決める。未知のフィールドはエラーにする。
- API の型は server と client で同じ Go の型を使い、食い違いを防ぐ。
- 外部向けの API として互換性は約束しない。CLI と server の版が違うときに分かるよう、応答に版を含める。

| メソッド | パス | 役割 | 権限 |
|---|---|---|---|
| POST | `/v1/watches` | 作成して開始 | operate |
| GET | `/v1/watches` | 一覧 | read |
| GET | `/v1/watches/{id}` | 定義と状態 | read |
| POST | `/v1/watches/{id}/stop` | 停止 | operate |
| DELETE | `/v1/watches/{id}` | 結果ごと削除 | operate |
| POST | `/v1/watches/{id}/targets` | ターゲット追加 | operate |
| DELETE | `/v1/watches/{id}/targets/{target_id}` | ターゲット削除 | operate |
| GET | `/v1/watches/{id}/summary` | 要約 | read |
| GET | `/v1/watches/{id}/events?after=CURSOR` | 前回より後の出来事 | read |
| GET | `/v1/watches/{id}/timeseries` | 間引いた推移 | read |
| POST | `/v1/watches/{id}/marks` | 印を付ける | operate |
| GET | `/v1/watches/{id}/compare?from=MARK&to=MARK` | 印の間の比較 | read |
| GET | `/v1/watches/{id}/metrics` | attach 用の全ターゲットの統計 | read |
| GET | `/healthz` | 死活確認 | 不要 |

ターゲットは URL を含むので、パスには server が付けた `target_id` を使う。

### summary

- 異常のあるターゲットを先頭に並べ、正常なものは件数にまとめる。
- 項目名は、今の `batch -o json`（`internal/ui/shared/export.go`）にそろえる。
- 保存の失敗や `gap` がある場合は、必ず含める。データが欠けたまま「正常」と読まれないようにするため。

### events

- cursor は server が振る連番とする。時刻だと、同時刻の出来事や時計の補正で取りこぼすため。
- 種類は次の通り。

| 種類 | 元になる情報 |
|---|---|
| `outage_start` / `outage_end` | `outageTracker` |
| `reply_ttl_changed` | `ttlChangeTracker` |
| `peer_changed` | `ConnInfo.RemoteAddr` |
| `certificate_changed` | `ConnInfo.TLS` |
| `target_added` / `target_removed` | API |
| `mark` | API |
| `watch_started` / `watch_stopped` / `gap` / `storage_error` | server |

- outage や TTL の変化は、遅れて届く応答を待ってから確定する（今の settle 時間）。出来事の時刻と、記録された時刻の両方を返す。
- DNS ラウンドロビンで接続先が毎回変わると `peer_changed` があふれる。TTL の変化と同じ規則（3回連続、直近50応答で見た値なら10回連続）で抑える。証明書も同じ。

### 外部由来の文字列

HTTP ヘッダー、証明書名、DNS の応答は、相手が書ける文字列である。AI に読ませるので、次のようにする。

- 既定では返さない。必要な人が明示的に指定したときだけ返す。
- 長さに上限を設け、制御文字を取り除く。
- 例外として、接続先の IP と証明書の期限は既定で返す。文字列ではなく、IP と時刻として扱えるため。

## 6. 認証とアクセス制御 **[案]**

| 待ち受け | 用途 | 認証 |
|---|---|---|
| Unix ソケット（既定） | 自分用 | ファイルの権限 |
| TCP | チーム共有、k8s | Bearer トークン必須 |

- 127.0.0.1 は、踏み台のほかのユーザーも接続できるので既定にしない。
- Ingress の認証だけに頼らない。クラスタ内から Service に直接つながれるため、TCP では必ずトークンを求める。
- TLS は Ingress などで終端する前提とし、mping では持たない。
- トークンには名前と権限（`read` / `operate`）を付け、作成者や mark の記録に名前を残す。トークンはファイル（k8s では Secret）から読む。

### ターゲット文字列だけで起こせること

prober の設定を外から変えられなくても、次のことは起こせる。HTTP のメソッドは GET だけで、リクエスト本文はない。

| 起きること | 対策 |
|---|---|
| 設定ヘッダーを付ける prober で、任意のホストにヘッダーを送らせる | prober ごとの許可する宛先 |
| リダイレクト先に設定ヘッダーが渡る | 修正済み（#90、v0.15.0） |
| 内部への GET（クラウドのメタデータ、GET で動く管理画面） | 許可する宛先。リダイレクト先も検査する |
| ポートスキャンやアドレス範囲のスキャン | 許可する宛先、ターゲット数の上限 |
| 過負荷（間隔は1ms から、HTTP は1回に最大64MiB 読む） | 間隔の下限、全体の送信レート、HTTP の読み取り上限 |
| AI が文書などに誘導され、意図しない宛先を指定する | 許可する宛先、AI には `read` だけを渡せること |

### server の設定

```yaml
server:
  allow_targets: ["10.0.0.0/8", "*.example.com"]
  limits:
    max_watches: 20
    max_targets_per_watch: 1024
    min_interval: 200ms
    max_probes_per_second: 2000
    max_duration: 24h
  retention: 720h
  tokens_file: /etc/mping/tokens
```

- 数値はすべて仮の値。
- `allow_targets` が空の場合、Unix ソケットでは制限なし、TCP では起動を拒否する。
- 検査は名前だけでなく、**実際に接続する IP** に対して行う。名前だけだと、DNS の応答を変えることで避けられるため。

## 7. 保存 **[案]**

SQLite（`modernc.org/sqlite`、純 Go）を使う。手元で残さない場合は、同じコードでメモリ上の SQLite を使う。

| 保存するもの | 保存しないもの |
|---|---|
| watch の定義、作成者 | probe の1件ずつ（直近100件はメモリに持つ） |
| event、mark | HTTP ヘッダー、応答本文 |
| 10秒ごとの集計（送信、成功、RTT の最小・平均・最大・p95） | 設定ファイルの中身、トークン |

- event は正確な時刻で残すので、集計が10秒単位でも停止時間の精度は落ちない。
- p95 は後から足し合わせられない。1分などに間引くときは、10秒ごとの p95 の最大を示し、そう明記する。
- 書き込みは別の goroutine でまとめて行い、**probe を待たせない**。詰まったら捨てて `storage_error` を残す。保存先が使えない間も、メモリ上の状態には出す。
- 停止した watch は、保存期限（案は30日）を過ぎたら削除する。
- スキーマには版を持たせ、起動時に移行する。

### 測定

Mac で、100ターゲット、1秒間隔、2時間ぶんを書いた結果（WAL、`synchronous=NORMAL`）。

| 方式 | 行数 | 容量 | 1回の書き込み |
|---|---|---|---|
| 1件ずつ | 720,000 | 16.3MB | 平均 1.2ms、最大 37ms |
| 10秒ごとの集計 | 72,000 | 2.9MB | 平均 1.2ms、最大 4ms |

- 1秒間隔なら、集計の容量は100ターゲットで1日あたり約35MB。
- バイナリは約4.6MB 増える（最小構成での比較）。
- cgo なしで、Linux・Windows・macOS の amd64 と arm64、Linux・Windows の 386 でビルドできた。
- k8s のボリューム上の速度は測っていない。

## 8. attach **[案]**

TUI の一覧は `stats.MetricsProvider`（`SortBy`）を通して統計を読んでいる。API から取得した統計でこれを実装すれば、一覧と詳細の大部分を流用できる見込み。

そのままでは済まない点は次の通り。

- **型**: `NewTUIApp` は `MetricsManager` を受け取り、イベントの購読まで含む。attach には多すぎるので、読み取りとリセットだけの小さな interface に分ける。
- **リセット（`R`）**: 共有中は他の人の統計を消さないよう、自分の表示だけの基準点にする。最初の版では無効にする。
- **path view（`p`）**: 今は TUI のプロセスが trace を打つ。attach では、server から打たないと見たい場所の経路にならない。最初の版では無効にする。
- **ビープとミュート**: 手元だけの動作にする。`OnFailure` は使えないので、event から鳴らす。
- **切断**: 再接続し、古い値であることを画面に出す。

## 9. 実装方針

| 項目 | 今の状態 | 必要な変更 |
|---|---|---|
| 独立した実行単位 | `session` が `ProbeManager` と `MetricsManager` の組 | `internal/command` から独立したパッケージに移す |
| 設定の共有 | `newSession` が `SetSourceInterface` で共有の `Config` を書き換える | watch ごとに複製する |
| 実行中の追加 | `AddTargets` は開始後エラー | `runPeriodic`（TCP、HTTP、DNS、NTP）と ICMP を、動的な一覧に変える |
| 実行中の削除 | なし | prober と stats の両方に足す |
| 追加時の名前解決 | `Accept` の中で行い、manager のロックを持ったまま待つ | ロックの外で、期限付きで行う |
| 出来事の通知 | なし（失敗ごとの `OnFailure` だけ） | stats から event を出すフックを足す |
| 長めの集計 | 履歴100件 | 10秒ごとの集計を足す |
| ICMP のソケット | prober ごとに1つ（datagram 優先） | そのまま |

パッケージの案は次の通り。

```
internal/session  probe と統計の組（今の internal/command の session を移す）
internal/watch    watch の管理（作成、停止、期限、再開）
internal/store    SQLite
internal/server   net/http の API、認証、上限
internal/client   API クライアント、attach 用の統計
```

## 10. root なしの ICMP（v0.15.0 で実装済み）

probe（#91）と trace（#92）が datagram ICMP ソケットで動く。server を動かすのに `CAP_NET_RAW` は要らない。

- Linux では `net.ipv4.ping_group_range` に、実行するユーザーのグループが入っている必要がある。この設定はネットワーク名前空間ごとである。
- 許可されていない場合は raw ソケットに切り替わる。
- Linux では応答がソケットごとに届くので、watch が増えても無駄な受信は増えない。macOS と raw では、ホスト宛ての応答がすべて届く。

## 11. k8s での配置 **[案]**

- 1 replica、`strategy: Recreate`。ローリング更新で、2つの Pod が同時に probe を打ったり、同じ DB を開いたりするのを避ける。
- ブロック型の PVC を使う。NFS や EFS は、ファイルロックの面で SQLite に向かない。
- `NET_RAW` は付けず、Pod の `securityContext.sysctls` で `net.ipv4.ping_group_range` を指定する（safe sysctl）。
- 終了時は probe を止め、未保存の集計を書いてから終わる。
- 見えるのは Pod のネットワークからの経路である。ノードからの見え方が必要なら `hostNetwork` を使う。
- コンテナイメージは今ないので、新しく用意する。

## 12. 段階

| 段階 | 内容 | 完了の条件 |
|---|---|---|
| 0 | `session` の分離、設定の複製、event フック（着手） | 今の CLI の動作が変わらない |
| 1 | `mping server`、作成・一覧・停止、`summary`、`events`。Unix ソケット、メモリ保存 | 実際の作業で AI が状態と変化を読める |
| 2 | SQLite、再起動後の再開、保存期限 | 再起動しても結果が残り、`gap` が outage にならない |
| 3 | TCP、トークン、許可する宛先、上限、コンテナイメージ | k8s でチーム共有できる |
| 4 | `watch attach` | 手元の TUI で同じ一覧と詳細が見える |
| 5 | 実行中のターゲット追加・削除 | 他のターゲットの計測が途切れない |
| 6 | `marks`、`compare`、`timeseries` | 切替の前後を比較できる |
| 7 | MCP | AI から直接使える |

- 段階1で一度実際の作業に使い、API を見直してから先に進む。
- 段階3より前に、TCP での公開は入れない。
- 当初のスナップショット案（A）と作業前後の比較（C）は、段階2と6に含まれる。
- 4 以降の順番は入れ替えられる。

## 13. 未決事項

1. 期限、保存期限、上限の値
2. MCP を自前で作るか、ライブラリを使うか
3. `allow_targets` で拒否したときに、ターゲット名を誰まで見せるか
4. Windows での待ち受け（Unix ソケットの権限の扱いが違う）
5. attach の更新を、定期取得と SSE のどちらにするか
6. server から trace を打つ API をいつ入れるか
7. 1件ずつの保存を、watch ごとの指定で許すか
