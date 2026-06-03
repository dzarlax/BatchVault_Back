package demoseed

import "time"

const DatasetVersion = "2026-06-03.1"

const ConfirmValue = "seed-demo-accounts"

const defaultDemoPassword = "BatchVaultDemo2026!"

var baseDate = time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

type accountFixture struct {
	Username      string
	PasswordEnv   string
	WorkspaceName string
	Ingredients   []ingredientFixture
	Recipes       []recipeFixture
	Packages      []packageFixture
	Products      []productFixture
	Clients       []clientFixture
	Orders        []orderFixture
	Sessions      []sessionFixture
}

type ingredientFixture struct {
	Key      string
	Name     string
	Type     string
	Alias    string
	Category string
	Prices   []priceFixture
}

type priceFixture struct {
	Price    float64
	Quantity float64
	Unit     string
	DaysAgo  int
}

type recipeFixture struct {
	Key         string
	Name        string
	Ingredients []recipeIngredientFixture
}

type recipeIngredientFixture struct {
	IngredientKey string
	Quantity      string
	Unit          string
}

type packageFixture struct {
	Key  string
	Name string
}

type productFixture struct {
	Key         string
	Name        string
	Description string
	Price       float64
	Cost        float64
	PackageKey  string
	RecipeKeys  []string
}

type clientFixture struct {
	Key       string
	Name      string
	Surname   string
	Phone     string
	Address   string
	Source    string
	Telegram  string
	Instagram string
}

type orderFixture struct {
	Key       string
	ClientKey string
	Status    string
	Comment   string
	DaysAgo   int
	Items     []orderItemFixture
}

type orderItemFixture struct {
	ProductKey string
	Quantity   int
	Price      float64
	CostPrice  float64
}

type sessionFixture struct {
	Key       string
	RecipeKey string
	Yield     string
	DaysAgo   int
}

var accountFixtures = []accountFixture{
	{
		Username:      "demo-en",
		PasswordEnv:   "DEMO_EN_PASSWORD",
		WorkspaceName: "Demo Smokehouse",
		Ingredients: []ingredientFixture{
			{Key: "beef", Name: "Beef topside", Type: "meat", Alias: "Lean beef", Category: "meat", Prices: []priceFixture{{Price: 1850, Quantity: 1, Unit: "kg", DaysAgo: 10}, {Price: 1980, Quantity: 1, Unit: "kg", DaysAgo: 2}}},
			{Key: "salt", Name: "Sea salt", Type: "spice", Alias: "Fine sea salt", Category: "spice", Prices: []priceFixture{{Price: 220, Quantity: 1, Unit: "kg", DaysAgo: 12}}},
			{Key: "paprika", Name: "Smoked paprika", Type: "spice", Alias: "Smoked paprika", Category: "spice", Prices: []priceFixture{{Price: 540, Quantity: 250, Unit: "g", DaysAgo: 8}}},
			{Key: "bag", Name: "Kraft pouch 100 g", Type: "packaging", Alias: "100 g pouch", Category: "packaging", Prices: []priceFixture{{Price: 18, Quantity: 1, Unit: "pcs", DaysAgo: 7}}},
		},
		Recipes: []recipeFixture{
			{Key: "classic", Name: "Classic beef jerky", Ingredients: []recipeIngredientFixture{{IngredientKey: "beef", Quantity: "5", Unit: "kg"}, {IngredientKey: "salt", Quantity: "90", Unit: "g"}, {IngredientKey: "paprika", Quantity: "45", Unit: "g"}}},
			{Key: "smoky", Name: "Smoky paprika strips", Ingredients: []recipeIngredientFixture{{IngredientKey: "beef", Quantity: "4", Unit: "kg"}, {IngredientKey: "salt", Quantity: "72", Unit: "g"}, {IngredientKey: "paprika", Quantity: "80", Unit: "g"}}},
		},
		Packages: []packageFixture{{Key: "pouch100", Name: "100 g kraft pouch"}, {Key: "gift3", Name: "Three-pack gift box"}},
		Products: []productFixture{
			{Key: "classic100", Name: "Classic jerky 100 g", Description: "Lean beef jerky in a kraft pouch", Price: 690, Cost: 380, PackageKey: "pouch100", RecipeKeys: []string{"classic"}},
			{Key: "giftbox", Name: "Jerky tasting box", Description: "Three pouches for gifts and events", Price: 1890, Cost: 1080, PackageKey: "gift3", RecipeKeys: []string{"classic", "smoky"}},
		},
		Clients: []clientFixture{
			{Key: "anna", Name: "Anna", Surname: "Petrova", Phone: "+381 60 111 222", Address: "Belgrade", Source: "Instagram", Instagram: "@anna_food"},
			{Key: "milan", Name: "Milan", Surname: "Jovanovic", Phone: "+381 64 333 444", Address: "Novi Sad", Source: "Market stand"},
		},
		Orders: []orderFixture{
			{Key: "order-ready", ClientKey: "anna", Status: "ready", Comment: "Pickup after work", DaysAgo: 1, Items: []orderItemFixture{{ProductKey: "classic100", Quantity: 3, Price: 690, CostPrice: 380}}},
			{Key: "order-new", ClientKey: "milan", Status: "new", Comment: "Gift box for Friday", DaysAgo: 0, Items: []orderItemFixture{{ProductKey: "giftbox", Quantity: 1, Price: 1890, CostPrice: 1080}}},
		},
		Sessions: []sessionFixture{{Key: "batch-classic", RecipeKey: "classic", Yield: "32 pouches", DaysAgo: 3}, {Key: "batch-smoky", RecipeKey: "smoky", Yield: "24 pouches", DaysAgo: 1}},
	},
	{
		Username:      "demo-sr",
		PasswordEnv:   "DEMO_SR_PASSWORD",
		WorkspaceName: "Demo domaca proizvodnja",
		Ingredients: []ingredientFixture{
			{Key: "beef", Name: "Junece meso", Type: "meat", Alias: "Nemasan juneći but", Category: "meat", Prices: []priceFixture{{Price: 1850, Quantity: 1, Unit: "kg", DaysAgo: 10}, {Price: 1980, Quantity: 1, Unit: "kg", DaysAgo: 2}}},
			{Key: "salt", Name: "Morska so", Type: "spice", Alias: "Sitna morska so", Category: "spice", Prices: []priceFixture{{Price: 220, Quantity: 1, Unit: "kg", DaysAgo: 12}}},
			{Key: "paprika", Name: "Dimljena paprika", Type: "spice", Alias: "Dimljena paprika", Category: "spice", Prices: []priceFixture{{Price: 540, Quantity: 250, Unit: "g", DaysAgo: 8}}},
			{Key: "bag", Name: "Kraft kesica 100 g", Type: "packaging", Alias: "Kesica 100 g", Category: "packaging", Prices: []priceFixture{{Price: 18, Quantity: 1, Unit: "pcs", DaysAgo: 7}}},
		},
		Recipes: []recipeFixture{
			{Key: "classic", Name: "Klasicna juneća sušena traka", Ingredients: []recipeIngredientFixture{{IngredientKey: "beef", Quantity: "5", Unit: "kg"}, {IngredientKey: "salt", Quantity: "90", Unit: "g"}, {IngredientKey: "paprika", Quantity: "45", Unit: "g"}}},
			{Key: "smoky", Name: "Dimljene paprika trake", Ingredients: []recipeIngredientFixture{{IngredientKey: "beef", Quantity: "4", Unit: "kg"}, {IngredientKey: "salt", Quantity: "72", Unit: "g"}, {IngredientKey: "paprika", Quantity: "80", Unit: "g"}}},
		},
		Packages: []packageFixture{{Key: "pouch100", Name: "Kraft kesica 100 g"}, {Key: "gift3", Name: "Poklon kutija od tri komada"}},
		Products: []productFixture{
			{Key: "classic100", Name: "Klasicne trake 100 g", Description: "Junece sušeno meso u kraft kesici", Price: 690, Cost: 380, PackageKey: "pouch100", RecipeKeys: []string{"classic"}},
			{Key: "giftbox", Name: "Degustaciona kutija", Description: "Tri kesice za poklon i događaje", Price: 1890, Cost: 1080, PackageKey: "gift3", RecipeKeys: []string{"classic", "smoky"}},
		},
		Clients: []clientFixture{
			{Key: "ana", Name: "Ana", Surname: "Petrovic", Phone: "+381 60 111 222", Address: "Beograd", Source: "Instagram", Instagram: "@ana_hrana"},
			{Key: "milan", Name: "Milan", Surname: "Jovanovic", Phone: "+381 64 333 444", Address: "Novi Sad", Source: "Pijaca"},
		},
		Orders: []orderFixture{
			{Key: "order-ready", ClientKey: "ana", Status: "ready", Comment: "Preuzimanje posle posla", DaysAgo: 1, Items: []orderItemFixture{{ProductKey: "classic100", Quantity: 3, Price: 690, CostPrice: 380}}},
			{Key: "order-new", ClientKey: "milan", Status: "new", Comment: "Poklon kutija za petak", DaysAgo: 0, Items: []orderItemFixture{{ProductKey: "giftbox", Quantity: 1, Price: 1890, CostPrice: 1080}}},
		},
		Sessions: []sessionFixture{{Key: "batch-classic", RecipeKey: "classic", Yield: "32 kesice", DaysAgo: 3}, {Key: "batch-smoky", RecipeKey: "smoky", Yield: "24 kesice", DaysAgo: 1}},
	},
	{
		Username:      "demo-ru",
		PasswordEnv:   "DEMO_RU_PASSWORD",
		WorkspaceName: "Демо домашнее производство",
		Ingredients: []ingredientFixture{
			{Key: "beef", Name: "Говядина верхняя часть", Type: "meat", Alias: "Постная говядина", Category: "meat", Prices: []priceFixture{{Price: 1850, Quantity: 1, Unit: "kg", DaysAgo: 10}, {Price: 1980, Quantity: 1, Unit: "kg", DaysAgo: 2}}},
			{Key: "salt", Name: "Морская соль", Type: "spice", Alias: "Мелкая морская соль", Category: "spice", Prices: []priceFixture{{Price: 220, Quantity: 1, Unit: "kg", DaysAgo: 12}}},
			{Key: "paprika", Name: "Копченая паприка", Type: "spice", Alias: "Копченая паприка", Category: "spice", Prices: []priceFixture{{Price: 540, Quantity: 250, Unit: "g", DaysAgo: 8}}},
			{Key: "bag", Name: "Крафт пакет 100 г", Type: "packaging", Alias: "Пакет 100 г", Category: "packaging", Prices: []priceFixture{{Price: 18, Quantity: 1, Unit: "pcs", DaysAgo: 7}}},
		},
		Recipes: []recipeFixture{
			{Key: "classic", Name: "Классическая вяленая говядина", Ingredients: []recipeIngredientFixture{{IngredientKey: "beef", Quantity: "5", Unit: "kg"}, {IngredientKey: "salt", Quantity: "90", Unit: "g"}, {IngredientKey: "paprika", Quantity: "45", Unit: "g"}}},
			{Key: "smoky", Name: "Копченые полоски с паприкой", Ingredients: []recipeIngredientFixture{{IngredientKey: "beef", Quantity: "4", Unit: "kg"}, {IngredientKey: "salt", Quantity: "72", Unit: "g"}, {IngredientKey: "paprika", Quantity: "80", Unit: "g"}}},
		},
		Packages: []packageFixture{{Key: "pouch100", Name: "Крафт пакет 100 г"}, {Key: "gift3", Name: "Подарочная коробка на три пачки"}},
		Products: []productFixture{
			{Key: "classic100", Name: "Классический джерки 100 г", Description: "Постная вяленая говядина в крафт пакете", Price: 690, Cost: 380, PackageKey: "pouch100", RecipeKeys: []string{"classic"}},
			{Key: "giftbox", Name: "Подарочный набор джерки", Description: "Три пачки для подарков и мероприятий", Price: 1890, Cost: 1080, PackageKey: "gift3", RecipeKeys: []string{"classic", "smoky"}},
		},
		Clients: []clientFixture{
			{Key: "anna", Name: "Анна", Surname: "Петрова", Phone: "+381 60 111 222", Address: "Белград", Source: "Instagram", Instagram: "@anna_food"},
			{Key: "milan", Name: "Милан", Surname: "Йованович", Phone: "+381 64 333 444", Address: "Нови-Сад", Source: "Маркет"},
		},
		Orders: []orderFixture{
			{Key: "order-ready", ClientKey: "anna", Status: "ready", Comment: "Забрать после работы", DaysAgo: 1, Items: []orderItemFixture{{ProductKey: "classic100", Quantity: 3, Price: 690, CostPrice: 380}}},
			{Key: "order-new", ClientKey: "milan", Status: "new", Comment: "Подарочный набор к пятнице", DaysAgo: 0, Items: []orderItemFixture{{ProductKey: "giftbox", Quantity: 1, Price: 1890, CostPrice: 1080}}},
		},
		Sessions: []sessionFixture{{Key: "batch-classic", RecipeKey: "classic", Yield: "32 пакета", DaysAgo: 3}, {Key: "batch-smoky", RecipeKey: "smoky", Yield: "24 пакета", DaysAgo: 1}},
	},
}
