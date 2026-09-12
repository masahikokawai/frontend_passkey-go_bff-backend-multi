import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import LabelSelect from "./LabelSelect";
import type { Label } from "./types";

// LabelSelectはCDNで読み込むjQuery/Select2(window.jQuery)に依存しており、jsdomには実体が存在しない
// ここでは`select2()`/`val()`/`on("change")`/`trigger()`だけを持つ最小限のフェイクをwindow.jQueryに差し込んでテストする
function installFakeJQuery() {
  const state: { value: string[]; changeHandler: (() => void) | null } = {
    value: [],
    changeHandler: null,
  };
  const fakeJQueryObject = {
    select2: vi.fn((_arg?: string) => fakeJQueryObject),
    on: vi.fn((event: string, handler: () => void) => {
      if (event === "change") state.changeHandler = handler;
      return fakeJQueryObject;
    }),
    val: vi.fn((next?: string[]) => {
      if (next === undefined) return state.value;
      state.value = next;
      return fakeJQueryObject;
    }),
    trigger: vi.fn(() => fakeJQueryObject),
  };
  const fakeJQuery = vi.fn(() => fakeJQueryObject);
  window.jQuery = fakeJQuery;
  return { fakeJQueryObject, state };
}

const options: Label[] = [
  { id: 1, name: "重要" },
  { id: 2, name: "緊急" },
];

describe("LabelSelect", () => {
  beforeEach(() => {
    // テスト間で状態を持ち越さないよう毎回消す
    delete window.jQuery;
  });

  it("Select2のchangeイベント発火でonChangeが選択中のIDの配列(number[])で呼ばれる", () => {
    const { state } = installFakeJQuery();
    const onChange = vi.fn();
    render(<LabelSelect options={options} value={[]} onChange={onChange} />);

    // Select2側で複数選択された状態を模擬してからchangeイベントを発火する
    state.value = ["1", "2"];
    state.changeHandler?.();

    expect(onChange).toHaveBeenCalledWith([1, 2]);
  });

  it("valueプロップが外部から変わるとSelect2側(val())にも反映される", () => {
    const { fakeJQueryObject } = installFakeJQuery();
    const onChange = vi.fn();
    const { rerender } = render(<LabelSelect options={options} value={[]} onChange={onChange} />);

    rerender(<LabelSelect options={options} value={[1]} onChange={onChange} />);

    expect(fakeJQueryObject.val).toHaveBeenCalledWith(["1"]);
    expect(fakeJQueryObject.trigger).toHaveBeenCalledWith("change.select2");
  });

  it("アンマウント時にselect2('destroy')が呼ばれる", () => {
    const { fakeJQueryObject } = installFakeJQuery();
    const { unmount } = render(
      <LabelSelect options={options} value={[]} onChange={vi.fn()} />
    );

    unmount();

    expect(fakeJQueryObject.select2).toHaveBeenCalledWith("destroy");
  });
});
