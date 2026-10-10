package provider

import "testing"

func TestProfile(t *testing.T) {
	p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ru-1": "https://s3.ru-1.storage.selcloud.ru", "ru-3": "https://s3.ru-3.storage.selcloud.ru", "ru-6": "https://s3.ru-6.storage.selcloud.ru", "ru-7": "https://s3.ru-7.storage.selcloud.ru", "gis-1": "https://s3.gis-1.storage.selcloud.ru", "uz-2": "https://s3.uz-2.srvstorage.uz"}
	if len(p.Pools) != len(want) {
		t.Fatalf("pool count: %d", len(p.Pools))
	}
	for _, pool := range p.Pools {
		if pool.Endpoint != want[pool.ID] {
			t.Errorf("endpoint %s: %s", pool.ID, pool.Endpoint)
		}
	}
	for _, f := range []string{"versioning", "object_lock_config", "cors", "bucket_policy", "conditional_writes", "multipart", "presign"} {
		if !p.Features[f] {
			t.Errorf("missing feature %s", f)
		}
	}
	for _, f := range []string{"object_retention", "object_tagging", "bucket_tagging", "lifecycle", "acl"} {
		if p.Features[f] {
			t.Errorf("unsupported feature %s", f)
		}
	}
	if p.PresignMaxSeconds != 604800 || p.Verified {
		t.Fatal("presign assumption lost")
	}
}
