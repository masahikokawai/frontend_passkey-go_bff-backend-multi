DELETE user_passwords FROM user_passwords
JOIN users ON users.id = user_passwords.user_id
WHERE users.email = 'local-user@example.com';

DELETE FROM users WHERE email = 'local-user@example.com';
