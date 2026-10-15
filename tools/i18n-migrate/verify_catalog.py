#!/usr/bin/env python3
"""校验 catalogs 与迁移前的旧文案逐字一致。

迁移（自写 i18n → MyGo 官方 ARB catalogs）完成后留下的回归脚本：

1. `tools/i18n-migrate/keys.json` 是从旧的四份 `catalog.go` 抽出来的原始取值
   （old_key → {zh, en, verbs}），`mapping.tsv` 是 old_key → new_key 对照表。
2. 把新 catalogs 的 ICU 文案「反格式化」回旧的 `%s`/`%d`/`%q`/`%v`，与旧取值
   逐字比对——防止有人悄悄改动中文/英文措辞（中文取值是用户看惯的界面文字，
   改动等于改产品）。
3. 反向检查新 catalogs 里没有多余的 key。

有意删除的 key 登记在 DELETED 里（本文件是唯一台账）：删掉文案时必须在这里
登记理由，脚本会断言它确实不在新 catalogs 里。

用法：python tools/i18n-migrate/verify_catalog.py
"""
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
sys.stdout.reconfigure(encoding="utf-8")

# ICU 占位：{name} 或 {name, number, ::group-off}；%q 外面还有一对引号。
ARG = re.compile(r"\{([A-Za-z][A-Za-z0-9_]*)(?:,\s*number,\s*::group-off)?\}")

# DELETED 是迁移完成后有意删除的旧 key：它们的唯一调用点随之消失，文案留着
# 也没有人取（`mygo i18n check` 不报「没人用的 key」）。
DELETED = {
    # 全文页「代码块超过行数上限」的提示：调用点在自写 markdown 渲染器里，
    # 官方 ui/markdown 没有行数上限，随该渲染器一起退役。
    "ui.markdown.moreLines",
}


def camel(old):
    """旧的点分 key → 新的扁平 camelCase key（与生成物里的函数名同形）。"""
    parts = old.split(".")
    return parts[0] + "".join(p[:1].upper() + p[1:] for p in parts[1:])


def main():
    keys = json.load(open(os.path.join(ROOT, "tools", "i18n-migrate", "keys.json"), encoding="utf-8"))
    en = json.load(open(os.path.join(ROOT, "locales", "en.json"), encoding="utf-8"))
    zh = json.load(open(os.path.join(ROOT, "locales", "zh-Hans.json"), encoding="utf-8"))
    bad = 0
    for old, info in sorted(keys.items()):
        if old in DELETED:
            for lang, table in (("zh", zh), ("en", en)):
                if camel(old) in table:
                    print("NOT DELETED %s %s" % (lang, camel(old)))
                    bad += 1
            continue
        nk = camel(old)
        for lang, table, orig in (("zh", zh, info["zh"]), ("en", en, info["en"])):
            if nk not in table:
                print("MISSING %s %s" % (lang, nk))
                bad += 1
                continue
            got = table[nk]
            if old == "cli.usage.text":
                got = got.replace("'{'", "{").replace("'}'", "}")
            # 反格式化：把 {name} 换回动词，%d 用 %d，%q 恢复引号。
            idx = [0]

            def repl(m):
                i = idx[0]
                idx[0] += 1
                verb = info["verbs"][i] if i < len(info["verbs"]) else "?"
                return "%" + verb

            back = ARG.sub(repl, got)
            # %q 的新文案把引号写进了正文（"{name}"），反格式化时去掉这对引号。
            back = back.replace('"%q"', "%q")
            if back != orig:
                print("DIFF %s %s\n  旧 %r\n  新 %r" % (lang, nk, orig, back))
                bad += 1
    # 反向：新 catalogs 里没有多余的 key
    old_new = {camel(k) for k in keys}
    for lang, table in (("zh", zh), ("en", en)):
        extra = set(table) - old_new - {"@@locale"}
        for k in sorted(extra):
            if k.startswith("@"):
                continue
            print("EXTRA %s %s" % (lang, k))
            bad += 1
    print("bad = %d / %d keys（有意删除的 %d 个见 DELETED）"
          % (bad, len(keys), len(DELETED)))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
