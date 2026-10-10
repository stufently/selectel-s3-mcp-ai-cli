# Selectel S3 compatibility

Generated from the embedded provider profile.

| Feature | Supported |
|---|---|
| acl | false |
| bucket_policy | true |
| bucket_tagging | false |
| conditional_delete | true |
| conditional_writes | true |
| cors | true |
| lifecycle | false |
| multipart | true |
| object_lock_config | true |
| object_retention | false |
| object_tagging | false |
| presign | true |
| versioning | true |

| Pool | Endpoint | Location | Multi-AZ | Classes |
|---|---|---|---|---|
| ru-1 | https://s3.ru-1.storage.selcloud.ru | Санкт-Петербург, Single-AZ | false | standard, cold |
| ru-3 | https://s3.ru-3.storage.selcloud.ru | Санкт-Петербург | false | standard, cold, ice |
| ru-6 | https://s3.ru-6.storage.selcloud.ru | Москва, Multi-AZ | true | standard, cold |
| ru-7 | https://s3.ru-7.storage.selcloud.ru | Москва, Single-AZ | false | standard, cold |
| gis-1 | https://s3.gis-1.storage.selcloud.ru | Москва, изолированный пул ГИС (ФСТЭК) | false | standard, cold |
| uz-2 | https://s3.uz-2.srvstorage.uz | Узбекистан | false | standard, cold |

| Class | Name | RUB/GB/month (from) | Verified | Checked |
|---|---|---|---|---|
| standard | Стандартное | 2.56 | false | 2026-10 |
| cold | Холодное | 1.25 | false | 2026-10 |
| ice | Ледяное | 0.89 | false | 2026-10 |

Presign maximum: 604800 seconds; verified: false. Потолок presigned не проверен; документация говорит о сроке жизни подписи запроса 15 минут.
