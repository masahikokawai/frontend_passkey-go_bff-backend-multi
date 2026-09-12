import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskList from "./TaskList";
import { fetchTasks, createTask, updateTask, deleteTask } from "./api";
import { apiFetch, ApiError } from "@/shared/api/client";
import type { Task } from "./types";

// LabelSelectはjQuery/Select2(CDN読み込み、jsdomには実体が無い)に依存するため、
// TaskList自体のロジック(一覧取得・編集開始・削除→再取得)を検証する範囲では
// スタブに差し替える(TaskForm.test.tsxと同じ方針)
vi.mock("./LabelSelect", () => ({
  default: () => <div data-testid="label-select-stub" />,
}));

// ./api・shared/api/clientは実ネットワークを叩かせず、呼び出しだけを検証する
vi.mock("./api", () => ({
  fetchTasks: vi.fn(),
  createTask: vi.fn(),
  updateTask: vi.fn(),
  deleteTask: vi.fn(),
}));
// apiFetch自体はモックするが、ApiErrorは実物を使う(401テストでnew ApiError(...)する必要があるため)
vi.mock("@/shared/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/api/client")>()),
  apiFetch: vi.fn(),
}));

const sampleTask: Task = {
  id: 1,
  name: "既存タスク",
  description: null,
  status: "waiting",
  finishedOn: "2030-01-01",
  labels: [{ id: 1, name: "重要" }],
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("TaskList(新実装)", () => {
  beforeEach(() => {
    vi.mocked(fetchTasks).mockResolvedValue({ tasks: [sampleTask], total: 1, limit: 20, offset: 0 });
    vi.mocked(apiFetch).mockResolvedValue({ labels: [{ id: 1, name: "重要" }] });
    vi.mocked(createTask).mockResolvedValue(sampleTask);
    vi.mocked(updateTask).mockResolvedValue(sampleTask);
    vi.mocked(deleteTask).mockResolvedValue(undefined);
  });

  it("マウント時にfetchTasksと/api/labelsを呼び、一覧・ラベル選択肢が表示される", async () => {
    render(<TaskList />);

    expect(await screen.findByText("既存タスク")).toBeInTheDocument();
    expect(fetchTasks).toHaveBeenCalledTimes(1);
    expect(apiFetch).toHaveBeenCalledWith("/api/labels");
  });

  it("行の「編集」ボタンを押すと、その行の値がフォームに反映される", async () => {
    render(<TaskList />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-edit-button"));

    expect(screen.getByTestId("task-name-input")).toHaveValue("既存タスク");
    expect(screen.getByTestId("task-finished-on-input")).toHaveValue("2030-01-01");
    expect(screen.getByTestId("task-submit-button")).toHaveTextContent("更新");
  });

  it("削除ボタン押下→確認OKでdeleteTaskが呼ばれ、一覧が再取得される", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<TaskList />);
    await screen.findByText("既存タスク");

    vi.mocked(fetchTasks).mockResolvedValueOnce({ tasks: [], total: 0, limit: 20, offset: 0 });
    await userEvent.click(screen.getByTestId("task-delete-button"));

    await waitFor(() => expect(deleteTask).toHaveBeenCalledWith(1));
    expect(fetchTasks).toHaveBeenCalledTimes(2);
  });

  it("削除が失敗した場合、エラーメッセージが表示される(未処理のPromise rejectionにならない)", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.mocked(deleteTask).mockRejectedValueOnce(new Error("削除できません"));
    render(<TaskList />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(await screen.findByTestId("delete-error-message")).toHaveTextContent("削除できません");
  });

  it("一覧取得が失敗した場合、エラーメッセージが表示される(読み込み中のまま固まらない)", async () => {
    vi.mocked(fetchTasks).mockRejectedValueOnce(new Error("一覧を取得できません"));
    render(<TaskList />);

    expect(await screen.findByTestId("load-error-message")).toHaveTextContent("一覧を取得できません");
  });

  // 【テスト監査で追加、セッション失効の一貫性確認】401(セッション失効)の実際の検知・
  // /loginへのリダイレクトはapiFetch自身の中で行われ、fetchTasks()はそれが既に発生した後の
  // ApiError(401)というrejectを受け取るだけ(client.test.tsxの「401の場合は...」で
  // 一元的に検証済み)。ここではその一元化されたrejectを、TaskListのようなAPI利用側の
  // コンポーネントが(重複した401ハンドリングコードを書かなくても)クラッシュせず
  // 素直にエラー状態として扱えることを確認する(リダイレクト完了までの一瞬の間、
  // 画面が壊れて見えないことの裏付け)
  it("一覧取得が401(セッション失効)で失敗しても、コンポーネントはクラッシュせずエラー表示になる", async () => {
    vi.mocked(fetchTasks).mockRejectedValueOnce(new ApiError(401, "unauthorized"));
    render(<TaskList />);

    expect(await screen.findByTestId("load-error-message")).toHaveTextContent("unauthorized");
  });

  it("タスクが0件のとき、テーブルの見出しだけが表示され行は出ない", async () => {
    vi.mocked(fetchTasks).mockResolvedValueOnce({ tasks: [], total: 0, limit: 20, offset: 0 });
    render(<TaskList />);

    await screen.findByText("タスク名");
    expect(screen.queryByTestId("task-row")).not.toBeInTheDocument();
  });

  // 【テスト監査で発見・修正】5言語のbackend実装のいずれかがlabelsをnull(空配列ではなく)で
  // 返す不具合があった場合でも、一覧全体がクラッシュせず、その行のラベル欄が空表示になるだけに
  // とどまることを確認する(型定義上はLabel[]だが、実際のJSONレスポンスが型と一致する保証は無い)
  it("タスクのlabelsがnullの場合でも一覧全体はクラッシュせず、ラベル欄が空で表示される", async () => {
    vi.mocked(fetchTasks).mockResolvedValueOnce({
      tasks: [{ ...sampleTask, labels: null as unknown as Task["labels"] }],
      total: 1,
      limit: 20,
      offset: 0,
    });
    render(<TaskList />);

    expect(await screen.findByText("既存タスク")).toBeInTheDocument();
    expect(screen.getByTestId("task-row")).toBeInTheDocument();
  });

  // 【テスト監査で発見・修正】TaskDeleteButtonのonDelete内では、deleteTask()が
  // 解決するまでreload()(=loading状態、画面全体が「読み込み中...」に置き換わる)には
  // 到達しない。そのため「削除ボタンを押した直後、deleteTask()自体がまだ解決していない間」は
  // 画面が操作可能なままであり、その隙に別の行を編集→更新することができる
  // (通信が遅い/混雑しているケースで実際に起こりうる)。
  //
  // この場合、reload()は「編集→更新」側が先に呼ばれ、「削除」側が
  // (deleteTask()の解決を待った分)後から呼ばれる。fetchTasksの応答は
  // リクエストが呼ばれた順に返ってくる保証が無いため、後発(削除側、より新しい状態を
  // 反映するはずのreload())の応答が先に返り、先発(更新側、削除前の古い状態を
  // 反映するreload())の応答がその後に返ると、新しい状態が古い状態で上書きされてしまう
  // バグがあった(TaskList.tsxのlatestRequestIdRefによる修正の回帰テスト)
  it("削除操作の完了待ち中に別の更新操作が先に完了しても、削除操作が最終的に完了した後の一覧が、更新操作由来の古い応答で上書きされない", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<TaskList />);
    await screen.findByText("既存タスク");

    // deleteTask自体を保留にする(reload()呼び出しより前の時点で止めるのが目的)
    let resolveDeleteTask!: () => void;
    vi.mocked(deleteTask).mockReturnValueOnce(
      new Promise<void>((resolve) => {
        resolveDeleteTask = resolve;
      })
    );

    type TasksResponse = { tasks: Task[]; total: number; limit: number; offset: number };
    let resolveUpdateReload!: (v: TasksResponse) => void;
    let resolveDeleteReload!: (v: TasksResponse) => void;
    vi.mocked(fetchTasks)
      .mockReturnValueOnce(
        new Promise<TasksResponse>((resolve) => {
          resolveUpdateReload = resolve;
        })
      )
      .mockReturnValueOnce(
        new Promise<TasksResponse>((resolve) => {
          resolveDeleteReload = resolve;
        })
      );

    // 削除ボタンを押す。deleteTask()がまだ解決しないため、reload()にはまだ到達せず
    // 画面は引き続き操作可能なまま(loadingにならない)
    await userEvent.click(screen.getByTestId("task-delete-button"));
    expect(deleteTask).toHaveBeenCalledTimes(1);

    // 削除の完了を待たずに、別の編集→更新を行う(reload()その1、fetchTasksの2回目の呼び出し)
    await userEvent.click(screen.getByTestId("task-edit-button"));
    await userEvent.click(screen.getByTestId("task-submit-button"));
    await waitFor(() => expect(updateTask).toHaveBeenCalledTimes(1));

    // ここでようやく削除が完了し、削除側のreload()が呼ばれる(fetchTasksの3回目の呼び出し=最新のreload())
    resolveDeleteTask();
    await waitFor(() => expect(fetchTasks).toHaveBeenCalledTimes(3));

    // 後発(削除側、最新)のreload()応答が先に返ってくる
    resolveDeleteReload({ tasks: [], total: 0, limit: 20, offset: 0 });
    await waitFor(() => expect(screen.queryByTestId("task-row")).not.toBeInTheDocument());

    // 先発(更新側、古い)のreload()応答が、削除側より遅れて返ってくる
    // 修正前はこれが一覧を上書きし、削除したはずのタスクが「更新後タスク」として復活して見えていた
    resolveUpdateReload({ tasks: [{ ...sampleTask, name: "更新後タスク" }], total: 1, limit: 20, offset: 0 });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByTestId("task-row")).not.toBeInTheDocument();
  });

  it("タスク名に<script>のような文字列が含まれていても、タグとして解釈されず文字列のまま表示される(JSXの自動エスケープ)", async () => {
    vi.mocked(fetchTasks).mockResolvedValueOnce({
      tasks: [{ ...sampleTask, name: "<script>alert(1)</script>" }],
      total: 1,
      limit: 20,
      offset: 0,
    });
    render(<TaskList />);

    expect(await screen.findByText("<script>alert(1)</script>")).toBeInTheDocument();
    expect(document.querySelectorAll("script")).toHaveLength(0);
  });

  // 【テスト監査で追加】backend側の調査で、NUL バイト(\x00)や他の制御文字(\x01等)を
  // 含むタスク名/descriptionが、Go(GORM/bob両方)・Railsとも切り詰め等を起こさず
  // そのままDBへ往復することを確認済み(5言語中Go・Railsで実DB検証、残りは未検証)。
  // そのようなデータがAPI応答として返ってきた場合に、React側のテキスト描画が
  // クラッシュしたり画面を壊したりしないことをここで固定する
  // (JSXは任意の文字列をそのままテキストノードとして扱うため理論上安全なはずだが、
  // 実際にテストで確認していなかった)
  it("タスク名にNULバイトや制御文字が含まれていても、クラッシュせずそのまま描画される", async () => {
    const nameWithControlChars = "a\x00bc\x01";
    vi.mocked(fetchTasks).mockResolvedValueOnce({
      tasks: [{ ...sampleTask, name: nameWithControlChars }],
      total: 1,
      limit: 20,
      offset: 0,
    });
    render(<TaskList />);

    const row = await screen.findByTestId("task-row");
    expect(row).toHaveAttribute("data-task-name", nameWithControlChars);
  });
});
