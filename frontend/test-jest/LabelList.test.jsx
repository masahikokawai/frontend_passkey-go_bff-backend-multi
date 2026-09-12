import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import LabelList from "../src/features/labels/LabelList";
import { fetchLabels, createLabel, updateLabel, deleteLabel } from "../src/features/labels/api";

// src/features/labels/LabelList.test.jsx (Vitest版)と同じ内容のJest版
jest.mock("../src/features/labels/api", () => ({
  __esModule: true,
  fetchLabels: jest.fn(),
  createLabel: jest.fn(),
  updateLabel: jest.fn(),
  deleteLabel: jest.fn(),
}));

const sampleLabel = { id: 1, name: "重要" };

describe("LabelList", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    fetchLabels.mockResolvedValue({ labels: [sampleLabel] });
    createLabel.mockResolvedValue({ id: 2, name: "新規" });
    updateLabel.mockResolvedValue({ id: 1, name: "更新後" });
    deleteLabel.mockResolvedValue(undefined);
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
    jest.spyOn(window, "confirm").mockReturnValue(true);
    render(<LabelList />);
    await screen.findByText("重要");

    fetchLabels.mockResolvedValueOnce({ labels: [] });
    await userEvent.click(screen.getByTestId("label-delete-button"));

    await waitFor(() => expect(deleteLabel).toHaveBeenCalledWith(1));
    expect(screen.getByTestId("success-message")).toHaveTextContent("ラベルを削除しました");
  });

  it("削除ボタン押下→確認キャンセルでdeleteLabelは呼ばれない", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(false);
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.click(screen.getByTestId("label-delete-button"));

    expect(deleteLabel).not.toHaveBeenCalled();
  });

  it("削除が失敗した場合、エラーメッセージが表示される(未処理のPromise rejectionにならない)", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(true);
    deleteLabel.mockRejectedValueOnce(new Error("削除できません"));
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.click(screen.getByTestId("label-delete-button"));

    expect(await screen.findByText("削除できません")).toBeInTheDocument();
  });

  it("createLabelが失敗した場合、エラーメッセージを表示する(成功メッセージは出ない)", async () => {
    createLabel.mockRejectedValueOnce(new Error("名前が重複しています"));
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.type(screen.getByTestId("label-name-input"), "重複");
    await userEvent.click(screen.getByTestId("label-submit-button"));

    expect(await screen.findByText("名前が重複しています")).toBeInTheDocument();
    expect(screen.queryByTestId("success-message")).not.toBeInTheDocument();
  });

  it("updateLabelが失敗した場合、エラーメッセージを表示し編集フォームを維持する", async () => {
    updateLabel.mockRejectedValueOnce(new Error("更新に失敗しました(サーバーエラー)"));
    render(<LabelList />);
    await screen.findByText("重要");

    await userEvent.click(screen.getByTestId("label-edit-button"));
    await userEvent.click(screen.getByTestId("label-save-button"));

    expect(await screen.findByText("更新に失敗しました(サーバーエラー)")).toBeInTheDocument();
    expect(screen.getByTestId("label-edit-input")).toBeInTheDocument();
  });

  it("一覧取得が失敗した場合、エラーメッセージが表示される(読み込み中のまま固まらない)", async () => {
    fetchLabels.mockRejectedValueOnce(new Error("一覧を取得できません"));
    render(<LabelList />);

    expect(await screen.findByText("一覧を取得できません")).toBeInTheDocument();
  });

  it("ラベルが0件のとき、見出しだけが表示され行は出ない", async () => {
    fetchLabels.mockResolvedValueOnce({ labels: [] });
    render(<LabelList />);

    await screen.findByText("ラベル一覧");
    expect(screen.queryByTestId("label-row")).not.toBeInTheDocument();
  });

  it("ラベル名に<script>のような文字列が含まれていても、タグとして解釈されず文字列のまま表示される(JSXの自動エスケープ)", async () => {
    fetchLabels.mockResolvedValueOnce({ labels: [{ id: 1, name: "<script>alert(1)</script>" }] });
    render(<LabelList />);

    expect(await screen.findByText("<script>alert(1)</script>")).toBeInTheDocument();
    expect(document.querySelectorAll("script")).toHaveLength(0);
  });
});
