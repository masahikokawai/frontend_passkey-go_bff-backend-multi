import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import LabelList from "./LabelList";
import { fetchLabels, createLabel, updateLabel, deleteLabel } from "./api";

// CONTRACT.mdセクション14: ラベルの一覧表示のみだった画面に登録・編集・削除を追加した分の検証
vi.mock("./api", () => ({
  fetchLabels: vi.fn(),
  createLabel: vi.fn(),
  updateLabel: vi.fn(),
  deleteLabel: vi.fn(),
}));

const sampleLabel = { id: 1, name: "重要" };

describe("LabelList", () => {
  beforeEach(() => {
    vi.mocked(fetchLabels).mockResolvedValue({ labels: [sampleLabel] });
    vi.mocked(createLabel).mockResolvedValue({ id: 2, name: "新規" });
    vi.mocked(updateLabel).mockResolvedValue({ id: 1, name: "更新後" });
    vi.mocked(deleteLabel).mockResolvedValue(undefined);
  });

  it("マウント時にfetchLabelsを呼び、一覧が表示される", async () => {
    render(<LabelList />);
    expect(await screen.findByText("重要")).toBeInTheDocument();
    expect(fetchLabels).toHaveBeenCalledTimes(1);
  });

  it("登録フォームに入力して送信するとcreateLabelが呼ばれ、成功メッセージが出る", async () => {
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.type(screen.getByTestId("label-name-input"), "新規");
    await userEvent.click(screen.getByTestId("label-submit-button"));

    await waitFor(() => expect(createLabel).toHaveBeenCalledWith("新規"));
    expect(screen.getByTestId("success-message")).toHaveTextContent("ラベルを作成しました");
  });

  it("編集ボタン→保存でupdateLabelが呼ばれ、成功メッセージが出る", async () => {
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.click(screen.getByTestId("label-edit-button"));
    const editInput = screen.getByTestId("label-edit-input");
    await userEvent.clear(editInput);
    await userEvent.type(editInput, "更新後");
    await userEvent.click(screen.getByTestId("label-save-button"));

    await waitFor(() => expect(updateLabel).toHaveBeenCalledWith(1, "更新後"));
    expect(screen.getByTestId("success-message")).toHaveTextContent("ラベルを更新しました");
  });

  it("削除ボタン押下→確認OKでdeleteLabelが呼ばれ、成功メッセージが出る", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<LabelList />);
    await screen.findByText("重要");

    vi.mocked(fetchLabels).mockResolvedValueOnce({ labels: [] });
    await userEvent.click(screen.getByTestId("label-delete-button"));

    await waitFor(() => expect(deleteLabel).toHaveBeenCalledWith(1));
    expect(screen.getByTestId("success-message")).toHaveTextContent("ラベルを削除しました");
  });

  it("削除ボタン押下→確認キャンセルでdeleteLabelは呼ばれない", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.click(screen.getByTestId("label-delete-button"));

    expect(deleteLabel).not.toHaveBeenCalled();
  });

  it("削除が失敗した場合、エラーメッセージが表示される(未処理のPromise rejectionにならない)", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.mocked(deleteLabel).mockRejectedValueOnce(new Error("削除できません"));
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.click(screen.getByTestId("label-delete-button"));

    expect(await screen.findByText("削除できません")).toBeInTheDocument();
  });

  it("createLabelが失敗した場合、エラーメッセージを表示する(成功メッセージは出ない)", async () => {
    vi.mocked(createLabel).mockRejectedValueOnce(new Error("名前が重複しています"));
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.type(screen.getByTestId("label-name-input"), "重複");
    await userEvent.click(screen.getByTestId("label-submit-button"));

    expect(await screen.findByText("名前が重複しています")).toBeInTheDocument();
    expect(screen.queryByTestId("success-message")).not.toBeInTheDocument();
  });

  it("updateLabelが失敗した場合、エラーメッセージを表示し編集フォームを維持する", async () => {
    vi.mocked(updateLabel).mockRejectedValueOnce(new Error("更新に失敗しました(サーバーエラー)"));
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.click(screen.getByTestId("label-edit-button"));
    await userEvent.click(screen.getByTestId("label-save-button"));

    expect(await screen.findByText("更新に失敗しました(サーバーエラー)")).toBeInTheDocument();
    // エラー時は編集モードのまま(editingIdをクリアしていない)
    expect(screen.getByTestId("label-edit-input")).toBeInTheDocument();
  });

  // 【テスト監査で発見・修正】TaskForm.tsxのsubmittingガードと同じ考え方の回帰テスト。
  // 以前はここに送信中のdisabledガードが無く、連打すると2つのcreateLabelが同時に飛び得た
  it("送信中は作成ボタンがdisabledになり、二重送信を防ぐ", async () => {
    let resolveCreate;
    vi.mocked(createLabel).mockReturnValueOnce(
      new Promise((resolve) => {
        resolveCreate = resolve;
      })
    );
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.type(screen.getByTestId("label-name-input"), "新規");
    const submitButton = screen.getByTestId("label-submit-button");
    expect(submitButton).not.toBeDisabled();

    await userEvent.click(submitButton);
    await waitFor(() => expect(submitButton).toBeDisabled());

    resolveCreate({ id: 2, name: "新規" });
    await waitFor(() => expect(submitButton).not.toBeDisabled());
    expect(createLabel).toHaveBeenCalledTimes(1);
  });

  // 【テスト監査で発見・修正】LabelList.jsxのlatestRequestIdRefによる修正の回帰テスト
  // (frontend/src/features/tasks/TaskList.test.tsxの同名テストと全く同じ理由)。
  // 削除ボタン押下後、deleteLabel()自体が解決するまでreload()(loading状態)には
  // 到達しないため、その間に別のラベルの作成を先に完了させることができる。この場合
  // reload()は「作成側」が先に呼ばれ「削除側」が後から呼ばれるため、削除側(最新)の応答が
  // 先に返り、作成側(古い)の応答がその後に返ると、新しい状態が古い状態で上書きされてしまう
  it("削除操作の完了待ち中に別の作成操作が先に完了しても、削除操作が最終的に完了した後の一覧が、作成操作由来の古い応答で上書きされない", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<LabelList />);
    await screen.findByText("重要");

    let resolveDeleteLabel;
    vi.mocked(deleteLabel).mockReturnValueOnce(
      new Promise((resolve) => {
        resolveDeleteLabel = resolve;
      })
    );

    let resolveCreateReload;
    let resolveDeleteReload;
    vi.mocked(fetchLabels)
      .mockReturnValueOnce(
        new Promise((resolve) => {
          resolveCreateReload = resolve;
        })
      )
      .mockReturnValueOnce(
        new Promise((resolve) => {
          resolveDeleteReload = resolve;
        })
      );

    // 削除ボタンを押す。deleteLabel()がまだ解決しないため、reload()にはまだ到達せず操作可能なまま
    await userEvent.click(screen.getByTestId("label-delete-button"));
    expect(deleteLabel).toHaveBeenCalledTimes(1);

    // 削除の完了を待たずに、別のラベルを作成する(reload()その1、fetchLabelsの2回目の呼び出し)
    await userEvent.type(screen.getByTestId("label-name-input"), "新規ラベル");
    await userEvent.click(screen.getByTestId("label-submit-button"));
    await waitFor(() => expect(createLabel).toHaveBeenCalledTimes(1));

    // ここでようやく削除が完了し、削除側のreload()が呼ばれる(fetchLabelsの3回目の呼び出し=最新)
    resolveDeleteLabel();
    await waitFor(() => expect(fetchLabels).toHaveBeenCalledTimes(3));

    // 後発(削除側、最新)のreload()応答が先に返ってくる: 残りは0件のはず
    resolveDeleteReload({ labels: [] });
    await waitFor(() => expect(screen.queryByTestId("label-row")).not.toBeInTheDocument());

    // 先発(作成側、古い)のreload()応答が、削除側より遅れて返ってくる
    // 修正前はこれが一覧を上書きし、削除したはずのラベルが「重要」として復活して見えていた
    resolveCreateReload({ labels: [sampleLabel, { id: 2, name: "新規ラベル" }] });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByTestId("label-row")).not.toBeInTheDocument();
  });

  it("一覧取得が失敗した場合、エラーメッセージが表示される(読み込み中のまま固まらない)", async () => {
    vi.mocked(fetchLabels).mockRejectedValueOnce(new Error("一覧を取得できません"));
    render(<LabelList />);

    expect(await screen.findByText("一覧を取得できません")).toBeInTheDocument();
  });

  it("ラベルが0件のとき、見出しだけが表示され行は出ない", async () => {
    vi.mocked(fetchLabels).mockResolvedValueOnce({ labels: [] });
    render(<LabelList />);

    await screen.findByText("ラベル一覧");
    expect(screen.queryByTestId("label-row")).not.toBeInTheDocument();
  });

  it("ラベル名に<script>のような文字列が含まれていても、タグとして解釈されず文字列のまま表示される(JSXの自動エスケープ)", async () => {
    vi.mocked(fetchLabels).mockResolvedValueOnce({ labels: [{ id: 1, name: "<script>alert(1)</script>" }] });
    render(<LabelList />);

    expect(await screen.findByText("<script>alert(1)</script>")).toBeInTheDocument();
    expect(document.querySelectorAll("script")).toHaveLength(0);
  });

  // 【テスト監査で発見・修正】新規作成/編集の入力欄がplaceholderのみで、
  // 正式なlabel(視覚上は非表示のvisually-hiddenラベル)が無かった。placeholderは
  // 入力中に消える上、スクリーンリーダーの読み上げ対象として保証されないため追加した
  it("新規作成欄がlabelから正しく参照できる(visually-hiddenラベルのhtmlFor/id関連付け)", async () => {
    render(<LabelList />);
    await screen.findByText("重要");

    expect(screen.getByLabelText("新しいラベル名(10文字以内)")).toBe(screen.getByTestId("label-name-input"));
  });

  it("編集欄がlabelから正しく参照できる(visually-hiddenラベルのhtmlFor/id関連付け)", async () => {
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.click(screen.getByTestId("label-edit-button"));

    expect(screen.getByLabelText("ラベル名(10文字以内)")).toBe(screen.getByTestId("label-edit-input"));
  });
});
