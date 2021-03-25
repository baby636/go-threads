package db

import (
	"context"
	"errors"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/textileio/go-threads/common"
	lstore "github.com/textileio/go-threads/core/logstore"
	"github.com/textileio/go-threads/core/thread"
	dutil "github.com/textileio/go-threads/db/util"
	"github.com/textileio/go-threads/util"
)

var (
	jsonSchema = `{
		"$schema": "http://json-schema.org/draft-04/schema#",
		"$ref": "#/definitions/person",
		"definitions": {
			"person": {
				"required": [
					"_id",
					"name",
					"age"
				],
				"properties": {
					"_id": {
						"type": "string"
					},
					"name": {
						"type": "string"
					},
					"age": {
						"type": "integer"
					}
				},
				"additionalProperties": false,
				"type": "object"
			}
		}
	}`
)

func TestManager_NewDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("test one new db", func(t *testing.T) {
		t.Parallel()
		man, clean := createTestManager(t)
		defer clean()
		_, err := man.NewDB(ctx, thread.NewRandomIDV1())
		checkErr(t, err)
	})
	t.Run("test multiple new dbs", func(t *testing.T) {
		t.Parallel()
		man, clean := createTestManager(t)
		defer clean()
		_, err := man.NewDB(ctx, thread.NewRandomIDV1())
		checkErr(t, err)
		// NewDB with token
		//sk, _, err := crypto.GenerateEd25519Key(rand.Reader)
		//checkErr(t, err)
		//tok, err := man.GetToken(ctx, thread.NewLibp2pIdentity(sk))
		//checkErr(t, err)
		//_, err = man.NewDB(ctx, thread.NewRandomIDV1(), WithNewManagedToken(tok))
		_, err = man.NewDB(ctx, thread.NewRandomIDV1())
		checkErr(t, err)
	})
	t.Run("test new db with bad name", func(t *testing.T) {
		t.Parallel()
		man, clean := createTestManager(t)
		defer clean()
		name := "my db"
		_, err := man.NewDB(ctx, thread.NewRandomIDV1(), WithNewManagedName(name))
		if err == nil {
			t.Fatal("new db with bad name should fail")
		}
	})
	t.Run("test new db with name", func(t *testing.T) {
		t.Parallel()
		man, clean := createTestManager(t)
		defer clean()
		name := "my-db"
		d, err := man.NewDB(ctx, thread.NewRandomIDV1(), WithNewManagedName(name))
		checkErr(t, err)
		if d.name != name {
			t.Fatalf("expected name %s, got %s", name, d.name)
		}
	})
}

func TestManager_GetDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dir, err := ioutil.TempDir("", "")
	checkErr(t, err)
	n, err := common.DefaultNetwork(
		common.WithNetBadgerPersistence(dir),
		common.WithNetHostAddr(util.FreeLocalAddr()),
		common.WithNetDebug(true),
	)
	checkErr(t, err)
	store, err := util.NewBadgerDatastore(dir, "eventstore")
	checkErr(t, err)
	man, err := NewManager(store, n, WithNewDebug(true))
	checkErr(t, err)
	defer func() {
		store.Close()
		_ = os.RemoveAll(dir)
	}()

	id := thread.NewRandomIDV1()
	_, err = man.GetDB(ctx, id)
	if !errors.Is(err, lstore.ErrThreadNotFound) {
		t.Fatal("should be not found error")
	}

	_, err = man.NewDB(ctx, id)
	checkErr(t, err)
	db, err := man.GetDB(ctx, id)
	checkErr(t, err)
	if db == nil {
		t.Fatal("db not found")
	}

	// Register a schema and create an instance
	collection, err := db.NewCollection(CollectionConfig{Name: "Person", Schema: dutil.SchemaFromSchemaString(jsonSchema)})
	checkErr(t, err)
	person1 := []byte(`{"_id": "", "name": "foo", "age": 21}`)
	_, err = collection.Create(person1)
	checkErr(t, err)

	time.Sleep(time.Second)

	// Close it down, restart next
	err = man.Close()
	checkErr(t, err)
	err = n.Close()
	checkErr(t, err)

	t.Run("test get db after restart", func(t *testing.T) {
		n, err := common.DefaultNetwork(
			common.WithNetBadgerPersistence(dir),
			common.WithNetHostAddr(util.FreeLocalAddr()),
			common.WithNetDebug(true),
		)
		checkErr(t, err)
		man, err := NewManager(store, n, WithNewDebug(true))
		checkErr(t, err)

		db, err := man.GetDB(ctx, id)
		checkErr(t, err)
		if db == nil {
			t.Fatal("db was not hydrated")
		}

		// Add another instance, this time there should be no need to register the schema
		collection := db.GetCollection("Person")
		if collection == nil {
			t.Fatal("collection was not hydrated")
		}
		person2 := []byte(`{"_id": "", "name": "bar", "age": 21}`)
		person3 := []byte(`{"_id": "", "name": "baz", "age": 21}`)
		_, err = collection.CreateMany([][]byte{person2, person3})
		checkErr(t, err)

		// Delete the db, we'll try to restart again
		err = man.DeleteDB(ctx, id)
		checkErr(t, err)

		time.Sleep(time.Second)

		err = man.Close()
		checkErr(t, err)
		err = n.Close()
		checkErr(t, err)

		t.Run("test get deleted db after restart", func(t *testing.T) {
			n, err := common.DefaultNetwork(
				common.WithNetBadgerPersistence(dir),
				common.WithNetHostAddr(util.FreeLocalAddr()),
				common.WithNetDebug(true),
			)
			checkErr(t, err)
			man, err := NewManager(store, n, WithNewDebug(true))
			checkErr(t, err)

			_, err = man.GetDB(ctx, id)
			if !errors.Is(err, lstore.ErrThreadNotFound) {
				t.Fatal("db was not deleted")
			}

			time.Sleep(time.Second)

			err = man.Close()
			checkErr(t, err)
			err = n.Close()
			checkErr(t, err)
		})
	})
}

func TestManager_DeleteDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	man, clean := createTestManager(t)
	defer clean()

	id := thread.NewRandomIDV1()
	db, err := man.NewDB(ctx, id)
	checkErr(t, err)

	// Register a schema and create an instance
	collection, err := db.NewCollection(CollectionConfig{Name: "Person", Schema: dutil.SchemaFromSchemaString(jsonSchema)})
	checkErr(t, err)
	person1 := []byte(`{"_id": "", "name": "foo", "age": 21}`)
	_, err = collection.Create(person1)
	checkErr(t, err)

	time.Sleep(time.Second)

	err = man.DeleteDB(ctx, id)
	checkErr(t, err)

	_, err = man.GetDB(ctx, id)
	if !errors.Is(err, lstore.ErrThreadNotFound) {
		t.Fatal("db was not deleted")
	}
}

func createTestManager(t *testing.T) (*Manager, func()) {
	dir, err := ioutil.TempDir("", "")
	checkErr(t, err)
	n, err := common.DefaultNetwork(
		common.WithNetBadgerPersistence(dir),
		common.WithNetHostAddr(util.FreeLocalAddr()),
		common.WithNetDebug(true),
	)
	checkErr(t, err)
	store, err := util.NewBadgerDatastore(dir, "eventstore")
	checkErr(t, err)
	m, err := NewManager(store, n, WithNewDebug(true))
	checkErr(t, err)
	return m, func() {
		if err := n.Close(); err != nil {
			panic(err)
		}
		if err := m.Close(); err != nil {
			panic(err)
		}
		if err := store.Close(); err != nil {
			panic(err)
		}
		_ = os.RemoveAll(dir)
	}
}
