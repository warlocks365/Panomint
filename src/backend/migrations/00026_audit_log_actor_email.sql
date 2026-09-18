-- 给 audit_log 补「操作者身份快照」列 actor_email —— 00025 的另一半。
--
-- **为什么要这一列（00025 只做了一半）**：
-- 00025 把 audit_log.user_id 的外键从 NO ACTION 放宽成 ON DELETE SET NULL，于是
-- 「账号一旦做过被审计的动作就永远删不掉」这个死结被解开了。但它只是解开死结，
-- 并没有补上原设计想要的另一样东西：**归因**。
--
-- 原设计（见 internal/auth/handlers.go / store.go 里 00025 之前的那段注释）之所以
-- 刻意用 NO ACTION，是为了「数据必须继续可归属，审计尤其不能因删用户而失去主体」。
-- 00025 之后，删账号行得通了，代价却是：该账号产生的审计行 user_id 变成 NULL，
-- 「这些动作是哪个账号做的」**永久丢失**，只留下 target_* 能说明被操作的对象。
-- 这等于给「先删账号以抹掉归因」留了口子 —— 而审计的全部价值就是归因可信。
--
-- **改法**：新增 actor_email，在**写入时**把操作者邮箱快照进这一列。
-- 它是一条**自包含的冗余**，不依赖 users 行是否还存在：
--   · 账号还在   → actor_email = 当时那个账号的邮箱，与 user_id 相互印证；
--   · 账号被删后 → user_id 被 00025 置 NULL，但 actor_email 原样保留，归因不丢。
-- 于是两条互相冲突的要求同时成立：
--   · 「账号可以删」（用户的要求）       —— 由 00025 的 SET NULL 满足；
--   · 「归因不丢」（原设计的要求）       —— 由本列的写入时快照满足。
--
-- 列可空是**硬要求**：审计列全部可空，且公开端点的审计本来就没有 actor
-- （见 文档/待解决问题记录.md §二十），此时子查询自然返回 NULL。
-- 绝不能让一个新列把「尽力而为」的审计写入变成可失败路径。
--
-- **历史行回填（本列的价值之一，但只回填能对上的那部分）**：
-- 下面的 UPDATE 把「user_id 仍指向一个存在的 users 行」的历史审计行补上邮箱。
-- 但必须说清它的边界：**回填只对「账号仍然存在」的行有效**。
-- 已经被删掉的账号（例如 admin2@pano.local —— 它在 00025 之后被删，
-- 其 4 行 auth.login 的 user_id 已被置为 NULL）**无法回填**：
-- 邮箱只存在于已被删除的 users 行里，库里再也找不到可据以还原的来源。
-- 这是本次改动之前就已经发生的**既成损失**，本迁移不编造、不猜测、
-- 不去 detail/ip 里凑一个假的邮箱。它的行会保持 actor_email IS NULL，
-- 语义诚实地表达「快照机制上线前，该操作者的身份已随账号删除而不可考」。
--
-- 只新增一列 + 回填，**不动**任何既有列、不动索引、不删除任何审计行。
-- 审计是只追加的账本：删行 = 篡改历史（本项目曾因验证脚本删审计行自造出 9 个空洞，
-- min=1 max=101 count=92），本迁移绝不重蹈。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS actor_email VARCHAR(255);

COMMENT ON COLUMN audit_log.actor_email IS '写入时快照的操作者邮箱。不依赖 users 行是否还存在，故账号被删除后归因仍在；公开端点（无 actor）为 NULL。';

-- 回填历史行：仅对「user_id 仍能对上 users.id」的行有效。
-- 已删账号的行（user_id 已被 00025 置 NULL）无法回填，保持 NULL（见文件头说明）。
UPDATE audit_log a SET actor_email = u.email FROM users u WHERE a.user_id = u.id AND a.actor_email IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 只回滚本迁移新增的那一列。回填进 actor_email 的值随列一起消失，
-- 但这不影响 user_id 与其余审计字段（它们从未被本迁移改动）。
ALTER TABLE audit_log DROP COLUMN IF EXISTS actor_email;
-- +goose StatementEnd
