import re

with open("../book/src/SUMMARY.md") as f:
    lines = f.readlines()

sections = {
    "operators":  r"super-sql/operators/(?!intro)",
    "aggregates": r"super-sql/aggregates/(?!intro)",
    "functions":  r"super-sql/functions/\w+/(?!intro)",
}

for label, pattern in sections.items():
    names = [re.search(r'\[([^\]]+)\]', l).group(1)
             for l in lines if re.search(pattern, l)]
    print(f"\n=== {label} ===")
    print("\n".join(names))
