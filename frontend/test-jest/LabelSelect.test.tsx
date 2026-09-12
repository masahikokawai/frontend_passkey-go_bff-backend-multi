import { render } from "@testing-library/react";
import LabelSelect from "../src/features/tasks/LabelSelect";
import type { Label } from "../src/features/tasks/types";

// src/features/tasks/LabelSelect.test.tsx (Vitest版)と同じ内容のJest版
// window.jQueryの最小フェイクはVitest版からそのまま流用している
//
// 【JestとVitestのts-jest差】
// ts-jestは実際に型検査を行うため、fakeJQueryObject が自分自身を参照するメソッド
// (select2/trigger)を型注釈なしで書くと「自分の初期化式内で自分の型を参照している」循環参照の型エラー(TS7022/TS7024)になる
// Vitestの既定transform(esbuild)は型検査を行わないため気づかなかった差異
// 明示的な型を与えて解消する
interface FakeJQueryObject {
  select2: jest.Mock;
  on: jest.Mock;
  val: jest.Mock;
  trigger: jest.Mock;
}

function installFakeJQuery() {
  const state: { value: string[]; changeHandler: (() => void) | null } = {
    value: [],
    changeHandler: null,
  };
  const fakeJQueryObject: FakeJQueryObject = {
    select2: jest.fn((_arg?: string): FakeJQueryObject => fakeJQueryObject),
    on: jest.fn((event: string, handler: () => void): FakeJQueryObject => {
      if (event === "change") state.changeHandler = handler;
      return fakeJQueryObject;
    }),
    val: jest.fn((next?: string[]): string[] | FakeJQueryObject => {
      if (next === undefined) return state.value;
      state.value = next;
      return fakeJQueryObject;
    }),
    trigger: jest.fn((): FakeJQueryObject => fakeJQueryObject),
  };
  const fakeJQuery = jest.fn(() => fakeJQueryObject);
  window.jQuery = fakeJQuery;
  return { fakeJQueryObject, state };
}

const options: Label[] = [
  { id: 1, name: "重要" },
  { id: 2, name: "緊急" },
];

describe("LabelSelect", () => {
  beforeEach(() => {
    delete window.jQuery;
  });

  it("Select2のchangeイベント発火でonChangeが選択中のIDの配列(number[])で呼ばれる", () => {
    const { state } = installFakeJQuery();
    const onChange = jest.fn();
    render(<LabelSelect options={options} value={[]} onChange={onChange} />);

    state.value = ["1", "2"];
    state.changeHandler?.();

    expect(onChange).toHaveBeenCalledWith([1, 2]);
  });

  it("valueプロップが外部から変わるとSelect2側(val())にも反映される", () => {
    const { fakeJQueryObject } = installFakeJQuery();
    const onChange = jest.fn();
    const { rerender } = render(<LabelSelect options={options} value={[]} onChange={onChange} />);

    rerender(<LabelSelect options={options} value={[1]} onChange={onChange} />);

    expect(fakeJQueryObject.val).toHaveBeenCalledWith(["1"]);
    expect(fakeJQueryObject.trigger).toHaveBeenCalledWith("change.select2");
  });

  it("アンマウント時にselect2('destroy')が呼ばれる", () => {
    const { fakeJQueryObject } = installFakeJQuery();
    const { unmount } = render(
      <LabelSelect options={options} value={[]} onChange={jest.fn()} />
    );

    unmount();

    expect(fakeJQueryObject.select2).toHaveBeenCalledWith("destroy");
  });
});
