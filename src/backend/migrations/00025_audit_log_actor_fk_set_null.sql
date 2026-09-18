-- 放宽 audit_log.user_id 的外键：NO ACTION → ON DELETE SET NULL
--
-- **为什么必须改**：审计是**只追加**的账本，而"不许删审计行"这条纪律与
-- "允许删除账号"这两件事在 NO ACTION 下**互相冲突**，结果是**账号一旦做过任何被审计的动作
-- 就永远删不掉**。这不是理论问题，本项目已经撞到两次：
--   · 3 个合成夹具账号（auditprobe / restoreprobe / historyprobe …）因成为 audit_log 的 actor
--     而无法 DELETE，只能长期以 status=disabled 的"僵尸行"留在库里；
--   · 2026-09-18 用户要求删除 admin2@pano.local（弱口令管理员账号），
--     实测被 `audit_log_user_id_fkey` 直接挡下（4 行 actor 记录 → SQLSTATE 23503）。
-- 于是同一份库里同时存在"不许删审计行"和"删不掉账号"两个约束，
-- 而唯一能绕过它的做法是**删掉那 4 行审计** —— 那正是本项目明令禁止的
-- （曾因验证脚本删审计行自造出 9 个空洞，min=1 max=101 count=92）。
--
-- **改法**：`ON DELETE SET NULL`。删账号后，该账号产生的审计行**原样保留**
-- （action / detail / ip / at / target_type / target_id 全在），只有 actor 引用变成 NULL，
-- 语义是"操作者账号已不存在"。列本就可空（00002 建表时 user_id 没有 NOT NULL），
-- 所以不需要额外改列定义。这也顺带覆盖了历史上"公开端点登录审计 actor 为 NULL"的既存形态
-- （见 文档/待解决问题记录.md §二十），读取侧本来就必须容忍 NULL actor。
--
-- **已知代价（必须写下来，别假装没有）**：audit_log 目前**没有**"操作者身份快照"列
-- （只有 user_id 外键）。因此删账号会**永久失去**"这些动作是哪个账号做的"这一信息——
-- 只有*账号本身作为被操作对象*的那条记录能靠 target_id / detail 追溯。
-- 理论上这给了"先删账号以抹掉归因"的空间。正式修法是给 audit_log 增加
-- `actor_email`（写入时快照），让归因不再依赖 users 行是否还在 —— 属独立改动，
-- 已登记为待办（见 文档/待解决问题记录.md），本次不夹带（避免把"能否删账号"与
-- "审计写入路径改造"两件事耦合在同一个提交里，后者要动 internal/audit 的写入口）。
--
-- 只改约束的删除规则，**不动**索引、不动其它任何外键。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_user_id_fkey;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL;

COMMENT ON COLUMN audit_log.user_id IS '操作者账号。账号被删除后置 NULL（操作留痕保留）；读取侧必须容忍 NULL。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 还原为默认的 NO ACTION。⚠️ 若此时已存在 user_id IS NULL 的行，本 Down **会失败**
-- （NO ACTION 不允许悬挂引用）—— 这是刻意的：还原前应先决定那些行怎么办，
-- 而不是让迁移静默把它们连带删掉。
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_user_id_fkey;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id);
-- +goose StatementEnd
