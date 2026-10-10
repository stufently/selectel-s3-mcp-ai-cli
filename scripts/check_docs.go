//go:build ignore

package main

import(
 "crypto/sha256"
 "fmt"
 "os"
 "os/exec"
 "strings"
)
func main(){
 headings:=map[string][]string{
  "README.md":{"# Selectel S3 MCP Server & AI CLI","## English","## Возможности","## Быстрый старт","## Подключение к Claude Code, Cursor, Codex","## Инструменты MCP","## CLI","## Skill для AI-агентов","## Безопасность","## Пулы и классы хранения Selectel","## FAQ","## Другие провайдеры","## Лицензия"},
  "README.en.md":{"# Selectel S3 MCP Server & AI CLI","## Features","## Quick start","## Connect to Claude Code, Cursor, Codex","## MCP tools","## CLI","## Agent skill","## Security","## Selectel pools and storage classes","## FAQ","## License"},
 }
 for path,required:=range headings{data,e:=os.ReadFile(path);if e!=nil{panic(e)};lines:="\n"+string(data)+"\n";for _,heading:=range required{if !strings.Contains(lines,"\n"+heading+"\n"){panic(path+": missing "+heading)}}}
 generated,e:=exec.Command(os.Args[1],"compat","--markdown").Output();if e!=nil{panic(e)}
 checked,e:=os.ReadFile("docs/compat.md");if e!=nil{panic(e)};if string(generated)!=string(checked){panic("compat output differs from docs/compat.md")}
 schema,e:=os.ReadFile("testdata/server.schema.json");if e!=nil{panic(e)};if fmt.Sprintf("%x",sha256.Sum256(schema))!="3fba09590c99f61735d234822279f4223fab9e300c0a81e81c91ab62a4114de0"{panic("schema sha256 differs")}
 fmt.Println("documentation headings, generated compatibility and Registry schema hash OK")
}
