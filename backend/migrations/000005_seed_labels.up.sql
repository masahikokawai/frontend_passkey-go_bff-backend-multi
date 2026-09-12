-- ラベルの複数選択(Task登録/編集フォーム)を試せるようにするための初期データ。
-- training-go/gin(参照元)の db/seeds.rb 相当(labelNames一覧)と同じ内容に合わせる。
INSERT INTO labels (name, created_at, updated_at) VALUES
    ('遊び', NOW(), NOW()),
    ('寝る', NOW(), NOW()),
    ('食べる', NOW(), NOW()),
    ('映画', NOW(), NOW()),
    ('旅行', NOW(), NOW()),
    ('掃除', NOW(), NOW()),
    ('仕事', NOW(), NOW()),
    ('ダイエット', NOW(), NOW()),
    ('ジム', NOW(), NOW());
