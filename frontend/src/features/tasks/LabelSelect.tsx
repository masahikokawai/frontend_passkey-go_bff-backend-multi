import { useEffect, useRef } from "react";
import type { Label } from "./types";

// training-go/gin(参照元)のtasks/_form.htmlは `<select multiple class="select2-select">` +
// jQuery Select2($('.select2-select').select2())でラベルの複数選択UIを実現している
// (素の<select multiple>はブラウザ既定でCtrl/Cmdキー併用が必要で使いづらいため)
// このコンポーネントは同じライブラリ(jQuery + Select2、index.htmlでCDN読み込み)を Reactから使うための薄いラッパー
// Select2自体はDOMを直接書き換えるため、React の宣言的な再描画とは別に、jQueryのAPIで値の同期を取る必要がある
declare global {
  interface Window {
    jQuery: any;
  }
}

interface LabelSelectProps {
  options: Label[];
  value: number[];
  onChange: (ids: number[]) => void;
}

export default function LabelSelect({ options, value, onChange }: LabelSelectProps) {
  const selectRef = useRef<HTMLSelectElement>(null);
  // onChangeを毎回の依存配列に入れるとSelect2の再初期化が無駄に走るため、
  // refに逃がして最新の関数だけ呼び出す(値自体の同期はvalueのuseEffectで行う)
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  // optionsが揃った(ラベル一覧取得後)タイミングでSelect2を初期化する
  useEffect(() => {
    const $ = window.jQuery;
    if (!$ || !selectRef.current) return;
    const $el = $(selectRef.current);
    $el.select2();
    $el.on("change", () => {
      const selected: string[] = $el.val() ?? [];
      onChangeRef.current(selected.map(Number));
    });
    return () => {
      $el.select2("destroy");
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [options]);

  // 新規作成⇔編集の切り替えなど、外部からvalueが変わった時にSelect2側の表示も同期する
  useEffect(() => {
    const $ = window.jQuery;
    if (!$ || !selectRef.current) return;
    const $el = $(selectRef.current);
    const current: number[] = ($el.val() ?? []).map(Number);
    const same =
      current.length === value.length && current.every((v: number) => value.includes(v));
    if (!same) {
      $el.val(value.map(String)).trigger("change.select2");
    }
  }, [value]);

  return (
    <select id="task-labels" ref={selectRef} multiple className="form-control" defaultValue={value.map(String)}>
      {options.map((label) => (
        <option key={label.id} value={label.id}>
          {label.name}
        </option>
      ))}
    </select>
  );
}
