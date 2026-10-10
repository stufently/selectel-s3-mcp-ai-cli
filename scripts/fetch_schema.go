//go:build ignore

package main

import (
 "context"
 "crypto/sha256"
 "fmt"
 "io"
 "net/http"
 "os"
 "time"
)
func main(){
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 request,e:=http.NewRequestWithContext(ctx,http.MethodGet,"https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",nil);if e!=nil{panic(e)}
 response,e:=http.DefaultClient.Do(request);if e!=nil{panic(e)};defer response.Body.Close();if response.StatusCode!=200{panic(response.Status)}
 data,e:=io.ReadAll(io.LimitReader(response.Body,1024*1024));if e!=nil{panic(e)}
 actual:=fmt.Sprintf("%x",sha256.Sum256(data));if actual!="3fba09590c99f61735d234822279f4223fab9e300c0a81e81c91ab62a4114de0"{panic("schema checksum mismatch: "+actual)}
 if e=os.WriteFile("testdata/server.schema.json",data,0600);e!=nil{panic(e)}
 fmt.Println(actual)
}
