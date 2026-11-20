# Корпус КТ2

Учебная выборка из **30 документов** (20 пунктов ПДД и 10 статей главы 12 КоАП). Это не полный юридический справочник: набор подобран под вопросы `benchmark/queries.json`. В интерфейсе объём базы обозначается как учебный.

## Происхождение и редакция

Тексты взяты 20.11.2026 со страниц КонсультантПлюс (раздел «Правила дорожного движения» и «КоАП РФ»), на которых указана последняя редакция:

- ПДД (Постановление Правительства РФ от 23.10.1993 № 1090) — ред. от 28.08.2026, https://www.consultant.ru/document/cons_doc_LAW_2709/;
- КоАП РФ — ред. от 04.08.2026, https://www.consultant.ru/document/cons_doc_LAW_34661/.

## Как подготовлено

- Пункт ПДД = один документ вместе с подпунктами и перечнями; статья КоАП = один документ со всеми частями, название статьи входит в текст.
- Удалены служебные пометки КонсультантПлюс (`(в ред. ...)`, `(см. текст в предыдущей редакции)`), сноски-отсылки `<*>` и абзацы «утратил силу»/«исключен». Сами нормы не менялись и не дополнялись; в ст. 12.9 нет ч. 1, так как она утратила силу.
- Порядок загрузки: `koap.json`, затем `pdd.json`, внутри файла — порядок массива.
- Скрипт скачивания не нужен: готовые `pdd.json` и `koap.json` лежат в репозитории, сервер и benchmark ничего не скачивают.

## Документы

| ID | Ссылка | Источник | Редакция |
|---|---|---|---|
| `pdd-2.1.1` | ПДД, п. 2.1.1 | https://www.consultant.ru/document/cons_doc_LAW_2709/a4b879c29ebc2ff9a56a0595499b6eb2dce7980e/ | 28.08.2026 |
| `pdd-2.1.2` | ПДД, п. 2.1.2 | https://www.consultant.ru/document/cons_doc_LAW_2709/a4b879c29ebc2ff9a56a0595499b6eb2dce7980e/ | 28.08.2026 |
| `pdd-2.3.2` | ПДД, п. 2.3.2 | https://www.consultant.ru/document/cons_doc_LAW_2709/a4b879c29ebc2ff9a56a0595499b6eb2dce7980e/ | 28.08.2026 |
| `pdd-2.7` | ПДД, п. 2.7 | https://www.consultant.ru/document/cons_doc_LAW_2709/a4b879c29ebc2ff9a56a0595499b6eb2dce7980e/ | 28.08.2026 |
| `pdd-5.1` | ПДД, п. 5.1 | https://www.consultant.ru/document/cons_doc_LAW_2709/ebac29ce89bbf83c3390095a126a259f746a0cb8/ | 28.08.2026 |
| `pdd-6.2` | ПДД, п. 6.2 | https://www.consultant.ru/document/cons_doc_LAW_2709/4b7a10a56ed37080fc96999db5f3db6f3aa58cc6/ | 28.08.2026 |
| `pdd-6.13` | ПДД, п. 6.13 | https://www.consultant.ru/document/cons_doc_LAW_2709/4b7a10a56ed37080fc96999db5f3db6f3aa58cc6/ | 28.08.2026 |
| `pdd-6.14` | ПДД, п. 6.14 | https://www.consultant.ru/document/cons_doc_LAW_2709/4b7a10a56ed37080fc96999db5f3db6f3aa58cc6/ | 28.08.2026 |
| `pdd-8.1` | ПДД, п. 8.1 | https://www.consultant.ru/document/cons_doc_LAW_2709/2fdfa8559de67744dab415a31c1f987bc016016b/ | 28.08.2026 |
| `pdd-10.1` | ПДД, п. 10.1 | https://www.consultant.ru/document/cons_doc_LAW_2709/803907f6876a655f77995888208488d4bfccf247/ | 28.08.2026 |
| `pdd-10.2` | ПДД, п. 10.2 | https://www.consultant.ru/document/cons_doc_LAW_2709/803907f6876a655f77995888208488d4bfccf247/ | 28.08.2026 |
| `pdd-11.1` | ПДД, п. 11.1 | https://www.consultant.ru/document/cons_doc_LAW_2709/00933a31260a2f952961558c8915b2304c0d37a5/ | 28.08.2026 |
| `pdd-11.4` | ПДД, п. 11.4 | https://www.consultant.ru/document/cons_doc_LAW_2709/00933a31260a2f952961558c8915b2304c0d37a5/ | 28.08.2026 |
| `pdd-12.1` | ПДД, п. 12.1 | https://www.consultant.ru/document/cons_doc_LAW_2709/59ded54942baa2143676e12b31244b0cb7ada00d/ | 28.08.2026 |
| `pdd-12.4` | ПДД, п. 12.4 | https://www.consultant.ru/document/cons_doc_LAW_2709/59ded54942baa2143676e12b31244b0cb7ada00d/ | 28.08.2026 |
| `pdd-13.3` | ПДД, п. 13.3 | https://www.consultant.ru/document/cons_doc_LAW_2709/74cbe820904f4f8ce76047ddbd81d14c8b953d3e/ | 28.08.2026 |
| `pdd-13.4` | ПДД, п. 13.4 | https://www.consultant.ru/document/cons_doc_LAW_2709/74cbe820904f4f8ce76047ddbd81d14c8b953d3e/ | 28.08.2026 |
| `pdd-13.9` | ПДД, п. 13.9 | https://www.consultant.ru/document/cons_doc_LAW_2709/74cbe820904f4f8ce76047ddbd81d14c8b953d3e/ | 28.08.2026 |
| `pdd-14.1` | ПДД, п. 14.1 | https://www.consultant.ru/document/cons_doc_LAW_2709/7c45508e360f5b7b8ae1443d73feb01f52a6199d/ | 28.08.2026 |
| `pdd-14.2` | ПДД, п. 14.2 | https://www.consultant.ru/document/cons_doc_LAW_2709/7c45508e360f5b7b8ae1443d73feb01f52a6199d/ | 28.08.2026 |
| `koap-12.3` | КоАП РФ, ст. 12.3 | https://www.consultant.ru/document/cons_doc_LAW_34661/0486252c9b58867b61fbeadf42daad5e67b324f1/ | 04.08.2026 |
| `koap-12.6` | КоАП РФ, ст. 12.6 | https://www.consultant.ru/document/cons_doc_LAW_34661/8854f2359dfca63486bc1efb9f95e6d2e4f003d4/ | 04.08.2026 |
| `koap-12.8` | КоАП РФ, ст. 12.8 | https://www.consultant.ru/document/cons_doc_LAW_34661/aa69183ecd988ed365aa7b0e5fffb687dc479b71/ | 04.08.2026 |
| `koap-12.9` | КоАП РФ, ст. 12.9 | https://www.consultant.ru/document/cons_doc_LAW_34661/85ebd6cb5138b31da96b1488716a764c41d50496/ | 04.08.2026 |
| `koap-12.12` | КоАП РФ, ст. 12.12 | https://www.consultant.ru/document/cons_doc_LAW_34661/8e1db11085c966408d1ce0191aef369706a76759/ | 04.08.2026 |
| `koap-12.13` | КоАП РФ, ст. 12.13 | https://www.consultant.ru/document/cons_doc_LAW_34661/6058a9a27e0e1ed5b479b663a20aae1cc8215bec/ | 04.08.2026 |
| `koap-12.14` | КоАП РФ, ст. 12.14 | https://www.consultant.ru/document/cons_doc_LAW_34661/b6f168f17b4b62158e01b87d868f571a3e539725/ | 04.08.2026 |
| `koap-12.15` | КоАП РФ, ст. 12.15 | https://www.consultant.ru/document/cons_doc_LAW_34661/3616f9cc443dbe11b6898b6fa10d5b67a307cb59/ | 04.08.2026 |
| `koap-12.18` | КоАП РФ, ст. 12.18 | https://www.consultant.ru/document/cons_doc_LAW_34661/b2daf1d5306cf52a5bb0ec8a1bc0ced772a4f162/ | 04.08.2026 |
| `koap-12.19` | КоАП РФ, ст. 12.19 | https://www.consultant.ru/document/cons_doc_LAW_34661/b9c0a2b651b7f06f5693cd2e77f04ff473f50f29/ | 04.08.2026 |

Страница источника для п. ПДД — раздел целиком, пункт ищется по номеру.
