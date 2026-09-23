defmodule BackendElixir.External.QueryTest do
  use ExUnit.Case, async: true

  alias BackendElixir.External.Query

  describe "parse_user_id/1" do
    test "accepts a valid numeric user_id" do
      assert {:ok, 42} = Query.parse_user_id(%{"user_id" => "42"})
    end

    test "rejects missing user_id" do
      assert {:error, %{kind: :user_id_required}} = Query.parse_user_id(%{})
    end

    test "rejects empty user_id" do
      assert {:error, %{kind: :user_id_required}} = Query.parse_user_id(%{"user_id" => ""})
    end

    test "rejects non-numeric user_id" do
      assert {:error, %{kind: :invalid_user_id}} = Query.parse_user_id(%{"user_id" => "abc"})
    end
  end

  describe "parse_offset_page/1" do
    test "defaults to page=1 page_size=10" do
      assert {1, 10} = Query.parse_offset_page(%{})
    end

    test "parses explicit page and page_size" do
      assert {3, 25} = Query.parse_offset_page(%{"page" => "3", "page_size" => "25"})
    end

    test "clamps page below 1 to the default" do
      assert {1, 10} = Query.parse_offset_page(%{"page" => "0"})
    end

    test "clamps non-numeric page_size to the default" do
      assert {1, 10} = Query.parse_offset_page(%{"page_size" => "abc"})
    end
  end

  describe "parse_cursor_page/1" do
    test "defaults to after_id=0 limit=10 when cursor is absent" do
      assert {0, 10} = Query.parse_cursor_page(%{})
    end

    test "parses explicit cursor and limit" do
      assert {99, 5} = Query.parse_cursor_page(%{"cursor" => "99", "limit" => "5"})
    end

    test "treats a non-numeric cursor as the start (after_id=0)" do
      assert {0, 10} = Query.parse_cursor_page(%{"cursor" => "not-a-number"})
    end

    test "clamps limit below 1 to the default" do
      assert {0, 10} = Query.parse_cursor_page(%{"limit" => "0"})
    end
  end
end
